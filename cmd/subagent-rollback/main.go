// Offline maintenance tool. Never connects to an Agent, provider or running Hub.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/zhangzongchu2019/dingwei/internal/store"
	"os"
)

func main() {
	path := flag.String("hub-db", "", "explicit stopped Hub SQLite path")
	root := flag.String("root-agent-id", "", "one durable root ID")
	offline := flag.Bool("writers-stopped", false, "confirm Hub and helper writers have stopped")
	off := flag.Bool("feature-off", false, "confirm creation and child admission are disabled")
	sealed := flag.Bool("helper-sealed", false, "confirm helper lanes sealed and individual returns settled")
	flag.Parse()
	if *path == "" || *root == "" || !*offline || !*off || !*sealed {
		fmt.Fprintln(os.Stderr, "requires explicit database/root, stopped writers, feature-off and helper-sealed confirmations")
		os.Exit(2)
	}
	info, err := os.Stat(*path)
	if err != nil || !info.Mode().IsRegular() {
		fmt.Fprintln(os.Stderr, "existing regular Hub database required")
		os.Exit(2)
	}
	db, err := store.OpenSQLite(*path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "database unavailable")
		os.Exit(1)
	}
	defer db.Close()
	result, err := db.PrepareSubAgentRollback(context.Background(), *root)
	if err != nil {
		fmt.Fprintln(os.Stderr, "rollback preparation failed; remain feature-off")
		os.Exit(1)
	}
	json.NewEncoder(os.Stdout).Encode(result)
}
