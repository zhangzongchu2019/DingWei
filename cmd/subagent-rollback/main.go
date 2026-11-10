// Offline maintenance tool. Never connects to an Agent, provider or running Hub.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/zhangzongchu2019/dingwei/internal/store"
	"io"
	"os"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("subagent-rollback", flag.ContinueOnError)
	flags.SetOutput(stderr)
	path := flags.String("hub-db", "", "explicit stopped Hub SQLite path")
	root := flags.String("root-agent-id", "", "one durable root ID")
	checkAll := flags.Bool("check-all", false, "read-only inventory of all unaudited child delivery sidecars")
	offline := flags.Bool("writers-stopped", false, "confirm Hub and helper writers have stopped")
	off := flags.Bool("feature-off", false, "confirm creation and child admission are disabled")
	sealed := flags.Bool("helper-sealed", false, "confirm helper lanes sealed and individual returns settled")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if *path == "" || flags.NArg() != 0 {
		fmt.Fprintln(stderr, "requires explicit database path and no positional arguments")
		return 2
	}
	if *checkAll && (*root != "" || *offline || *off || *sealed) {
		fmt.Fprintln(stderr, "check-all is read-only and cannot be combined with single-root preparation flags")
		return 2
	}
	if !*checkAll && (*root == "" || !*offline || !*off || !*sealed) {
		fmt.Fprintln(stderr, "requires explicit database/root, stopped writers, feature-off and helper-sealed confirmations")
		return 2
	}
	info, err := os.Stat(*path)
	if err != nil || !info.Mode().IsRegular() {
		fmt.Fprintln(stderr, "existing regular Hub database required")
		return 2
	}
	if *checkAll {
		db, err := store.OpenSQLiteReadOnly(*path)
		if err != nil {
			fmt.Fprintln(stderr, "read-only database unavailable")
			return 1
		}
		defer db.Close()
		result, err := db.CheckSubAgentRollback(context.Background())
		if err != nil {
			fmt.Fprintln(stderr, "full database check failed; remain feature-off")
			return 1
		}
		if err := json.NewEncoder(stdout).Encode(result); err != nil {
			return 1
		}
		if result.Pending != 0 {
			fmt.Fprintln(stderr, "unaudited child deliveries remain; binary downgrade forbidden")
			return 1
		}
		return 0
	}
	db, err := store.OpenSQLite(*path)
	if err != nil {
		fmt.Fprintln(stderr, "database unavailable")
		return 1
	}
	defer db.Close()
	result, err := db.PrepareSubAgentRollback(context.Background(), *root)
	if err != nil {
		fmt.Fprintln(stderr, "rollback preparation failed; remain feature-off")
		return 1
	}
	if err := json.NewEncoder(stdout).Encode(result); err != nil {
		return 1
	}
	return 0
}
