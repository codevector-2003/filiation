package store

import "testing"

func TestMeta(t *testing.T) {
	db := migrated(t)
	ctx := t.Context()

	got, err := db.Meta(ctx, "reader_tabs")
	if err != nil || got != "" {
		t.Fatalf("Meta before any write = %q, %v; want \"\", nil", got, err)
	}
	for _, want := range []string{`{"a":1}`, `{"a":2}`} {
		if err := db.Tx(ctx, func(tx *Tx) error { return tx.SetMeta(ctx, "reader_tabs", want) }); err != nil {
			t.Fatal(err)
		}
		if got, err := db.Meta(ctx, "reader_tabs"); err != nil || got != want {
			t.Errorf("Meta = %q, %v; want %q", got, err, want)
		}
	}

	if err := db.Tx(ctx, func(tx *Tx) error { return tx.SetMeta(ctx, metaSchemaVersion, "1") }); err == nil {
		t.Error("SetMeta changed the schema version")
	}
	if v, _ := db.Meta(ctx, metaSchemaVersion); v != "16" {
		t.Errorf("schema version = %q after a refused write, want 16", v)
	}
}
