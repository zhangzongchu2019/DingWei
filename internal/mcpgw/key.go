package mcpgw

import (
	"errors"
	"io"
	"os"
)

// ReadOAuthKeyFile 读取装配从 DW_MCPGW_OAUTH_KEY_FILE 取得的仓外文件路径。
// 推荐 systemd LoadCredential=mcpgw-oauth-key:<受控路径>，环境变量指向该 credential。
// 文件必须是私有的32字节原始随机密钥；错误不回传路径或文件内容。
func ReadOAuthKeyFile(path string) ([]byte, error) {
	invalid := errors.New("mcpgw: invalid OAuth key file")
	if path == "" {
		return nil, invalid
	}
	f, e := os.Open(path)
	if e != nil {
		return nil, invalid
	}
	defer f.Close()
	info, e := f.Stat()
	if e != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0177 != 0 {
		return nil, invalid
	}
	key, e := io.ReadAll(io.LimitReader(f, 33))
	if e != nil || len(key) != 32 {
		return nil, invalid
	}
	return key, nil
}
