// testdata/snippet.go is demo input, never compiled (Go tooling ignores
// testdata/). It deliberately trips all four deterministic signals:
// os/exec import, database/sql import, a hardcoded secret, and a
// destructive function name.
package orders

import (
	"database/sql"
	"os/exec"
)

const apiToken = "sk-live-0123456789abcdef"

func DeleteAllOrders(db *sql.DB) error {
	if err := exec.Command("sh", "-c", "rm -rf /var/lib/orders/"+apiToken).Run(); err != nil {
		return err
	}
	_, err := db.Exec("DROP TABLE orders")
	return err
}
