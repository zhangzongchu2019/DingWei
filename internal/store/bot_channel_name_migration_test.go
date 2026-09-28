package store

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/zhangzongchu2019/dingwei/internal/model"
)

func TestBotChannelDisplayNameMigrationPreservesChannelID(t *testing.T) {
	ctx := context.Background()
	db, err := OpenSQLite(filepath.Join(t.TempDir(), "bot-name-migration.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	for _, channel := range []model.BotChannel{
		{ID: "grey-id", Name: "GreyTestRobot", AppID: "cli_grey", Active: true},
		{ID: "other-id", Name: "OtherRobot", AppID: "cli_other", Active: true},
	} {
		if err := db.UpsertBotChannel(ctx, channel); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.db.ExecContext(ctx, `DELETE FROM schema_migrations WHERE version='0052_bot_channel_display_name.sql'`); err != nil {
		t.Fatal(err)
	}
	if ok, err := db.verifyDataMigration(ctx, "0052_bot_channel_display_name.sql"); err != nil || ok {
		t.Fatalf("legacy name still exists but verifier passed: ok=%t err=%v", ok, err)
	}
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	channels, err := db.ListBotChannels(ctx)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, channel := range channels {
		got[channel.ID] = channel.Name
	}
	if got["grey-id"] != "dingwei" {
		t.Fatalf("legacy bot display name was not migrated: got=%q want dingwei", got["grey-id"])
	}
	if got["other-id"] != "OtherRobot" {
		t.Fatalf("migration changed unrelated bot: got=%q", got["other-id"])
	}
	if ok, err := db.verifyDataMigration(ctx, "0052_bot_channel_display_name.sql"); err != nil || !ok {
		t.Fatalf("retired legacy name was not recognized as migrated: ok=%t err=%v", ok, err)
	}
}
