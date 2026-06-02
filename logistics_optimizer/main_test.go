package main

import (
	"context"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/duynguyendang/manglekit"
)

func repoRoot() string {
	_, filename, _, _ := runtime.Caller(0)
	return filepath.Dir(filepath.Dir(filename))
}

func TestTopologyPredicates(t *testing.T) {
	ctx := context.Background()
	client := manglekit.Must(manglekit.NewClient(ctx, manglekit.WithBlueprintPath(filepath.Join(repoRoot(), "logistics_optimizer/validator.dl"))))

	solutions, err := client.Engine().Query(ctx, nil, `is_right("1", "2").`)
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}
	if len(solutions) == 0 {
		t.Error("is_right(\"1\", \"2\") should be derivable")
	}

	solutions, err = client.Engine().Query(ctx, nil, `is_opposite("1", "3").`)
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}
	if len(solutions) == 0 {
		t.Error("is_opposite(\"1\", \"3\") should be derivable")
	}

	solutions, err = client.Engine().Query(ctx, nil, `is_next("1", "2").`)
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}
	if len(solutions) == 0 {
		t.Error("is_next(\"1\", \"2\") should be derivable")
	}

	solutions, err = client.Engine().Query(ctx, nil, `is_next("2", "1").`)
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}
	if len(solutions) == 0 {
		t.Error("is_next(\"2\", \"1\") should be derivable via is_right(\"1\", \"2\")")
	}

	solutions, err = client.Engine().Query(ctx, nil, `is_right("1", "4").`)
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}
	if len(solutions) != 0 {
		t.Error("is_right(\"1\", \"4\") should not be derivable")
	}
}
