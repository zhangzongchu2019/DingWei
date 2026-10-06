package nodeagent

// 账号登录（0.2.1.101，R-V2-ACCOUNT-LOGIN-FLOW；设计件 runbooks/DESIGN-R-V2-ACCOUNT-LOGIN-FLOW-opus50tc2k.md 第十二、十三节）：
// 节点起一次性登录容器跑 `codex login --device-auth`，从 attach 流里只取「链接＋一次性码」回给 Hub；
// 用户在自己浏览器完成后 CLI 写出 auth.json，节点读出后**主动 HTTPS 上传 Hub**（不走命令结果——结果帧 32KB 上限，
// 且命令结果本应入账本；这两个命令已列 noLedgerCommands 不入账本）。
//
// 🔴 凭证不落节点磁盘：CODEX_HOME 在宿主内存盘私有目录 <loginRoot>/login-<id>（0700、属主 10001），读后立即删；
//    容器 LogConfig=none（Docker 不落 stdout）；容器输出只在本进程内存里按固定模式取链接与码，原文不进日志。
// 🔴 幂等自保（不入账本）：容器名固定 login-<id>；同 id 重放 login_start 返回现状；login_cancel 对不存在的也成功。

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	loginLabel       = "dingwei.login"
	loginTTL         = 15 * time.Minute // = codex 一次性码有效期
	loginPerNodeMax  = 2                // 组长裁定：每节点并发 2
	loginMaxCredSize = 64<<10 + 4<<10   // 同 Hub 上传限长
	loginMemBytes    = 256 << 20
)

var (
	loginIDPattern   = regexp.MustCompile(`^[0-9a-f]{32}$`)
	loginDirPattern  = regexp.MustCompile(`^login-[0-9a-f]{32}$`)
	loginURLPattern  = regexp.MustCompile(`^https://\S+$`)
	loginCodePattern = regexp.MustCompile(`^[A-Z0-9]{4,}-[A-Z0-9]{4,}$`)
	// ErrLoginBusy 本节点进行中的登录已达上限。
	ErrLoginBusy = errors.New("login_busy")
)

// loginStartWait login_start 等链接与码的上限（超了即判失败、清场）；测试可调小。
var loginStartWait = 30 * time.Second

type loginSession struct {
	url, code string
	cancel    context.CancelFunc
	done      chan struct{}
}

type loginOps struct {
	d         *dockerOps
	root      string // 宿主内存盘根（生产 /dev/shm）
	hubOrigin string
	secretFn  func() (string, error)
	hc        *http.Client

	mu       sync.Mutex
	sessions map[string]*loginSession
}

// RegisterLoginHandlers 注册 login_start／login_cancel，并先清扫上次进程留下的登录容器、网络与内存盘目录。
func (a *Agent) RegisterLoginHandlers(eng *engine, registry, localImagesCSV, cgroupParent, seatProxy, hubWSS, loginRoot string, secretFn func() (string, error), certPin string) error {
	allow, err := ParseLocalImages(localImagesCSV)
	if err != nil {
		return err
	}
	origin, err := hubHTTPSOrigin(hubWSS)
	if err != nil {
		return err
	}
	hc := &http.Client{Timeout: 2 * time.Minute}
	if certPin != "" {
		tr, err := TLSPinnedTransport(certPin)
		if err != nil {
			return err
		}
		hc.Transport = tr
	}
	if loginRoot == "" {
		loginRoot = "/dev/shm"
	}
	l := &loginOps{
		d:    &dockerOps{eng: eng, registry: registry, localImages: allow, cgroupParent: cgroupParent, seatProxy: seatProxy},
		root: loginRoot, hubOrigin: origin, secretFn: secretFn, hc: hc, sessions: map[string]*loginSession{},
	}
	l.cleanupLeftovers(context.Background())
	a.Register(CmdLoginStart, l.start)
	a.Register(CmdLoginCancel, l.cancel)
	return nil
}

// hubHTTPSOrigin wss://host/path ⇒ https://host（同 export_seat 的换算）。
func hubHTTPSOrigin(hubWSS string) (string, error) {
	if !strings.HasPrefix(hubWSS, "wss://") {
		return "", fmt.Errorf("nodeagent: HubURL 必须 wss://")
	}
	origin := "https://" + strings.TrimPrefix(hubWSS, "wss://")
	if i := strings.Index(origin[len("https://"):], "/"); i >= 0 {
		origin = origin[:len("https://")+i]
	}
	return origin, nil
}

func loginNames(id string) (container, network string) { return "login-" + id, "loginnet-" + id }

func (l *loginOps) dir(id string) string { return filepath.Join(l.root, "login-"+id) }

func (l *loginOps) start(ctx context.Context, params json.RawMessage) (json.RawMessage, error) {
	var p struct {
		LoginID   string `json:"login_id"`
		Image     string `json:"image"`
		UploadURL string `json:"upload_url"`
	}
	if err := strictDecode(params, &p); err != nil {
		return nil, err
	}
	if !loginIDPattern.MatchString(p.LoginID) {
		return nil, fmt.Errorf("%w: login_id", ErrParams)
	}
	// 🔴 防外泄：上传地址只认本 Hub 的本会话端点
	if p.UploadURL != l.hubOrigin+"/api/node/login/"+p.LoginID {
		return nil, fmt.Errorf("%w: upload_url 非本 Hub 本会话登录端点", ErrParams)
	}
	if err := validateImageRef(p.Image, l.d.registry); err != nil {
		return nil, err
	}
	if _, err := l.d.checkLocalImage(ctx, p.Image); err != nil {
		return nil, err
	}

	l.mu.Lock()
	if s, ok := l.sessions[p.LoginID]; ok { // 重放：返回现状，不起第二个
		l.mu.Unlock()
		return loginStartResult(s.url, s.code)
	}
	if len(l.sessions) >= loginPerNodeMax {
		l.mu.Unlock()
		return nil, ErrLoginBusy
	}
	sess := &loginSession{done: make(chan struct{})}
	l.sessions[p.LoginID] = sess
	l.mu.Unlock()

	url, code, err := l.launch(ctx, p.LoginID, p.Image, p.UploadURL, sess)
	if err != nil {
		l.forget(p.LoginID)
		l.cleanup(context.Background(), p.LoginID)
		return nil, err
	}
	return loginStartResult(url, code)
}

func loginStartResult(url, code string) (json.RawMessage, error) {
	return json.Marshal(map[string]string{"url": url, "user_code": code})
}

// launch 建目录与网络、起容器、附着输出、等链接与码；成功后后台 finish 等退出并上传。
func (l *loginOps) launch(ctx context.Context, id, image, uploadURL string, sess *loginSession) (string, string, error) {
	dir := l.dir(id)
	if err := os.Mkdir(dir, 0o700); err != nil && !os.IsExist(err) {
		return "", "", fmt.Errorf("login_start_failed: workdir: %v", err)
	}
	if err := chownForSeat(dir); err != nil {
		return "", "", fmt.Errorf("login_start_failed: workdir owner: %v", err)
	}
	// codex 0.159.2 起 CODEX_HOME 不存在即拒启（「CODEX_HOME points to … but that path does not exist」，prodops 实测板 28152cb0；
	// 0.145.0 只告警）⇒ 先建 .codex（0700、属主 10001）再起容器。
	codexHome := filepath.Join(dir, ".codex")
	if err := os.Mkdir(codexHome, 0o700); err != nil && !os.IsExist(err) {
		return "", "", fmt.Errorf("login_start_failed: codex home: %v", err)
	}
	if err := chownForSeat(codexHome); err != nil {
		return "", "", fmt.Errorf("login_start_failed: codex home owner: %v", err)
	}
	cname, nname := loginNames(id)
	if err := l.d.eng.networkCreate(ctx, nname); err != nil {
		return "", "", fmt.Errorf("login_start_failed: network: %v", err)
	}
	cid, err := l.d.eng.containerCreate(ctx, cname, l.spec(id, image, dir, nname))
	if err != nil {
		return "", "", fmt.Errorf("login_start_failed: create: %v", err)
	}
	runCtx, cancel := context.WithTimeout(context.Background(), loginTTL)
	stream, err := l.d.eng.containerAttach(runCtx, cid)
	if err != nil {
		cancel()
		return "", "", fmt.Errorf("login_start_failed: attach: %v", err)
	}
	if err := l.d.eng.containerStart(ctx, cid); err != nil {
		stream.Close()
		cancel()
		return "", "", fmt.Errorf("login_start_failed: start: %v", err)
	}
	found := make(chan [2]string, 1)
	go func() { // 读输出：只认固定模式的链接行与码行；原文不留、不打日志；读完继续排空直到流结束
		var url, code string
		sent := false
		sc := bufio.NewScanner(demuxReader(stream))
		sc.Buffer(make([]byte, 0, 4096), 64<<10)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if url == "" && loginURLPattern.MatchString(line) {
				url = line
			} else if code == "" && loginCodePattern.MatchString(line) {
				code = line
			}
			if !sent && url != "" && code != "" {
				found <- [2]string{url, code}
				sent = true
			}
		}
		if !sent {
			close(found)
		}
	}()
	var got [2]string
	select {
	case v, ok := <-found:
		if !ok {
			stream.Close()
			cancel()
			return "", "", fmt.Errorf("login_start_failed: no_device_code") // 不带输出原文
		}
		got = v
	case <-time.After(loginStartWait):
		stream.Close()
		cancel()
		return "", "", fmt.Errorf("login_start_failed: device_code_timeout")
	}
	l.mu.Lock()
	sess.url, sess.code, sess.cancel = got[0], got[1], cancel
	l.mu.Unlock()
	go l.finish(runCtx, id, cid, uploadURL, stream, sess)
	return got[0], got[1], nil
}

func (l *loginOps) spec(id, image, dir, netName string) map[string]any {
	env := []string{"HOME=/h", "CODEX_HOME=/h/.codex"}
	if l.d.seatProxy != "" {
		env = append(env, "HTTPS_PROXY="+l.d.seatProxy, "HTTP_PROXY="+l.d.seatProxy, "https_proxy="+l.d.seatProxy, "http_proxy="+l.d.seatProxy)
	}
	return map[string]any{
		"Image":        engineImage(image),
		"User":         "10001:10001",
		"Entrypoint":   []string{"/usr/local/bin/codex"},
		"Cmd":          []string{"login", "--device-auth", "-c", `cli_auth_credentials_store="file"`},
		"Env":          env,
		"Tty":          false,
		"AttachStdout": true,
		"AttachStderr": true,
		"Labels":       map[string]string{loginLabel: id},
		"HostConfig": map[string]any{
			"ReadonlyRootfs": true,
			"Tmpfs":          map[string]string{"/tmp": "size=8m"},
			"Memory":         loginMemBytes,
			"MemorySwap":     loginMemBytes,
			"CpuShares":      256,
			"CgroupParent":   l.d.cgroupParent,
			"CapDrop":        []string{"ALL"},
			"SecurityOpt":    []string{"no-new-privileges"},
			"Privileged":     false,
			"NetworkMode":    netName,
			"Binds":          []string{dir + ":/h"}, // 只挂本会话内存盘目录；不挂任何席位卷
			"Init":           true,
			"LogConfig":      map[string]any{"Type": "none"}, // 🔴 Docker 不落 stdout（链接与码不进宿主盘）
		},
	}
}

// finish 等容器退出：成功 ⇒ 读 auth.json 上传；否则上传失败结果。最后清场。被取消（Hub 已知）则只清场不上传。
func (l *loginOps) finish(ctx context.Context, id, cid, uploadURL string, stream io.Closer, sess *loginSession) {
	defer close(sess.done)
	defer l.forget(id)
	defer l.cleanup(context.Background(), id)
	defer stream.Close()
	code, err := l.d.eng.containerWait(ctx, cid)
	if ctx.Err() == context.Canceled {
		return
	}
	outcome := map[string]string{"status": "failed"}
	switch {
	case ctx.Err() == context.DeadlineExceeded || err != nil:
		outcome["reason"] = "login_timeout"
	case code != 0:
		outcome["reason"] = fmt.Sprintf("login_exit_%d", code)
	default:
		cred, rerr := readLoginCredential(filepath.Join(l.dir(id), ".codex", "auth.json"))
		if rerr != nil {
			outcome["reason"] = rerr.Error()
		} else {
			outcome = map[string]string{"status": "ok", "credential": cred}
		}
	}
	if err := l.upload(uploadURL, outcome); err != nil {
		log.Printf("nodeagent: login %s 结果上传失败: %v", id, err) // 不带凭证
	}
}

// readLoginCredential 只认常规文件（不跟随链接）、限长、须为 JSON。错误文本不含内容。
func readLoginCredential(path string) (string, error) {
	fi, err := os.Lstat(path)
	if err != nil || !fi.Mode().IsRegular() {
		return "", errors.New("credential_missing")
	}
	if fi.Size() == 0 || fi.Size() > loginMaxCredSize {
		return "", errors.New("credential_size")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", errors.New("credential_unreadable")
	}
	if !json.Valid(raw) {
		return "", errors.New("credential_not_json")
	}
	return string(raw), nil
}

func (l *loginOps) upload(uploadURL string, outcome map[string]string) error {
	body, err := json.Marshal(outcome)
	if err != nil {
		return err
	}
	secret, err := l.secretFn()
	if err != nil {
		return err
	}
	var last error
	for attempt := 0; attempt < 3; attempt++ {
		req, err := http.NewRequest("POST", uploadURL, strings.NewReader(string(body)))
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+secret)
		req.Header.Set("Content-Type", "application/json")
		resp, err := l.hc.Do(req)
		if err != nil {
			last = err
			time.Sleep(time.Duration(attempt+1) * time.Second)
			continue
		}
		resp.Body.Close()
		if resp.StatusCode/100 == 2 {
			return nil
		}
		return fmt.Errorf("HTTP %d", resp.StatusCode) // 4xx（会话已结束／不符）不重试
	}
	return last
}

func (l *loginOps) cancel(ctx context.Context, params json.RawMessage) (json.RawMessage, error) {
	var p struct {
		LoginID string `json:"login_id"`
	}
	if err := strictDecode(params, &p); err != nil {
		return nil, err
	}
	if !loginIDPattern.MatchString(p.LoginID) {
		return nil, fmt.Errorf("%w: login_id", ErrParams)
	}
	l.mu.Lock()
	sess := l.sessions[p.LoginID]
	l.mu.Unlock()
	if sess != nil && sess.cancel != nil {
		sess.cancel()
		select { // finish 自己清场；等一会儿，等不到也照常再清一遍
		case <-sess.done:
		case <-time.After(15 * time.Second):
		}
	}
	l.cleanup(ctx, p.LoginID) // 不存在也成功（幂等）
	return json.Marshal(map[string]string{"login_id": p.LoginID, "state": "cancelled"})
}

func (l *loginOps) forget(id string) {
	l.mu.Lock()
	delete(l.sessions, id)
	l.mu.Unlock()
}

// cleanup 删容器、网络、内存盘目录；都按「不存在即成功」。
func (l *loginOps) cleanup(ctx context.Context, id string) {
	cname, nname := loginNames(id)
	_ = l.d.eng.containerRemove(ctx, cname)
	_ = l.d.eng.networkRemove(ctx, nname)
	_ = os.RemoveAll(l.dir(id))
}

// cleanupLeftovers 进程启动清扫：带登录标签的容器、对应网络；内存盘下名字严格匹配 login-<32hex> 且属主为席位用户的目录。
func (l *loginOps) cleanupLeftovers(ctx context.Context) {
	if items, err := l.d.eng.containersByLabel(ctx, loginLabel); err == nil {
		for _, c := range items {
			if id := c.Labels[loginLabel]; loginIDPattern.MatchString(id) {
				l.cleanup(ctx, id)
			}
		}
	}
	ents, err := os.ReadDir(l.root)
	if err != nil {
		return
	}
	for _, e := range ents {
		if !e.IsDir() || !loginDirPattern.MatchString(e.Name()) {
			continue
		}
		if !ownedBySeatUser(filepath.Join(l.root, e.Name())) {
			continue
		}
		l.cleanup(ctx, strings.TrimPrefix(e.Name(), "login-"))
	}
}

// demuxReader Docker 非 TTY 附着流：8 字节帧头（流号、3 字节 0、4 字节大端长度）＋载荷；stdout 与 stderr 合并为一条字节流。
func demuxReader(r io.Reader) io.Reader {
	pr, pw := io.Pipe()
	go func() {
		hdr := make([]byte, 8)
		for {
			if _, err := io.ReadFull(r, hdr); err != nil {
				pw.Close()
				return
			}
			n := binary.BigEndian.Uint32(hdr[4:])
			if _, err := io.CopyN(pw, r, int64(n)); err != nil {
				pw.CloseWithError(err)
				return
			}
		}
	}()
	return pr
}

// ownedBySeatUser 清扫前核属主＝席位运行用户（不碰内存盘上别人的同名目录）；测试可替换。
var ownedBySeatUser = func(path string) bool {
	fi, err := os.Lstat(path)
	if err != nil {
		return false
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	return ok && st.Uid == seatRunUID
}
