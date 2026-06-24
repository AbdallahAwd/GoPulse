package services

import (
	"testing"

	"github.com/amaa/gopulse/internal/models"
)

func TestMySQLExecCommandUsesRootControlCredential(t *testing.T) {
	db := models.Database{
		Type:     "mysql",
		Username: "app_user",
		Password: "pa'ss",
	}

	got := mysqlExecCommand("gopulse-db-1", db, "appdb", "-B -N", "SHOW DATABASES;", "2>/dev/null")
	want := "docker exec -e MYSQL_PWD='pa'\"'\"'ss' 'gopulse-db-1' mysql --default-character-set=utf8mb4 --user='root' --database='appdb' -B -N -e 'SHOW DATABASES;' 2>/dev/null"

	if got != want {
		t.Fatalf("mysqlExecCommand() = %q, want %q", got, want)
	}
}

func TestMySQLIdentifierEscapesBackticks(t *testing.T) {
	got := mysqlIdentifier("tenant`db")
	want := "`tenant``db`"

	if got != want {
		t.Fatalf("mysqlIdentifier() = %q, want %q", got, want)
	}
}
