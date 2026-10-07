package main

import (
	"context"
	"errors"
	"testing"

	"github.com/tansrai/tansr-go/executor"
)

func TestOrderToolHasNoGeneralCommandSurface(t *testing.T) {
	for _, args := range []map[string]any{{}, {"orderId": 3}, {"orderId": "DEMO-001", "command": "anything"}} {
		_, err := orderStatus(context.Background(), args)
		var rejected *executor.Rejected
		if !errors.As(err, &rejected) {
			t.Fatal("unexpected input reached the business handler")
		}
	}
	value, err := orderStatus(context.Background(), map[string]any{"orderId": "DEMO-001"})
	if err != nil || value.(map[string]any)["status"] != "ok" {
		t.Fatalf("sample failed: %v", err)
	}
	value, err = orderStatus(context.Background(), map[string]any{"orderId": "unknown"})
	if err != nil || value.(map[string]any)["status"] != "error" {
		t.Fatalf("missing order not represented: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := orderStatus(ctx, map[string]any{"orderId": "DEMO-001"}); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation ignored")
	}
}

func TestSampleDeclarationFitsSDKDefinition(t *testing.T) {
	digest, err := executor.DefinitionDigest(declaration())
	// Locked against the existing Serve demo declaration, computed with the
	// original Node clientToolDefinitionDigest implementation.
	if err != nil || digest != "fb0ae6b3fd7dbfca35be5f54694fa5d70bf9587234d5d559188fefaa86da65fd" {
		t.Fatalf("demo declaration changed: %s %v", digest, err)
	}
	value := declaration()
	if value["name"] != toolName || value["readOnly"] != true {
		t.Fatal("sample must remain explicitly read-only")
	}
}
