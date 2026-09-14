package naming_test

import (
	"testing"

	"github.com/jsazanami-del/pg2protobuf-generator/internal/naming"
)

func TestPascalCaseNoSingularize(t *testing.T) {
	if g := naming.PascalCase("users"); g != "Users" {
		t.Fatalf("got %q", g)
	}
	if g := naming.PascalCase("order_status"); g != "OrderStatus" {
		t.Fatalf("got %q", g)
	}
}

func TestFieldNameReserved(t *testing.T) {
	if g := naming.FieldName("message"); g != "message_" {
		t.Fatalf("got %q", g)
	}
	if g := naming.FieldName("created_at"); g != "created_at" {
		t.Fatalf("got %q", g)
	}
}

func TestEnumValueName(t *testing.T) {
	if g := naming.EnumValueName("OrderStatus", "pending"); g != "ORDER_STATUS_PENDING" {
		t.Fatalf("got %q", g)
	}
	if g := naming.UnspecifiedName("OrderStatus"); g != "ORDER_STATUS_UNSPECIFIED" {
		t.Fatalf("got %q", g)
	}
}
