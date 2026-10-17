package services

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/AbdallahAwd/GoPulse/internal/models"
)

func remoteBackupDumpCommand(db models.Database, runID string) (string, error) {
	if !safeResourceID.MatchString(db.ID) {
		return "", invalidBackup("Invalid database identifier.")
	}
	if _, err := uuidBackupID(runID); err != nil {
		return "", err
	}
	container := shellQuote("gopulse-" + db.ID)
	switch db.Type {
	case "postgres":
		return "docker exec -e PGPASSWORD=" + shellQuote(db.Password) + " " + container + " pg_dump --format=custom --no-owner --no-acl --username=" + shellQuote(db.Username) + " --dbname=" + shellQuote(db.DatabaseName), nil
	case "mysql":
		return "docker exec -e MYSQL_PWD=" + shellQuote(db.Password) + " " + container + " mysqldump --user=root --single-transaction --quick --routines --events --triggers --hex-blob --no-tablespaces --set-gtid-purged=OFF --column-statistics=0 -- " + shellQuote(db.DatabaseName), nil
	case "mongodb":
		return "docker exec " + container + " mongodump --quiet --archive --gzip --authenticationDatabase=admin --username=" + shellQuote(db.Username) + " --password=" + shellQuote(db.Password) + " --db=" + shellQuote(db.DatabaseName), nil
	case "redis":
		file := "/tmp/gopulse-rb-" + runID + ".rdb"
		return "trap " + shellQuote("docker exec "+container+" rm -f "+shellQuote(file)+" >/dev/null 2>&1 || true") + " EXIT\n" + "docker exec -e REDISCLI_AUTH=" + shellQuote(db.Password) + " " + container + " redis-cli --rdb " + shellQuote(file) + " >/dev/null\n" + "docker exec " + container + " cat " + shellQuote(file), nil
	default:
		return "", invalidBackup("Unsupported backup engine.")
	}
}
func remoteBackupRestoreScript(plan models.RemoteBackupPlan, a models.RemoteBackupArtifact, runID, work string) (string, error) {
	if _, err := uuidBackupID(runID); err != nil {
		return "", err
	}
	if a.Engine != "sqlite" && !backupImageID.MatchString(a.ImageID) {
		return "", invalidBackup("Invalid recovery image identifier.")
	}
	if a.Engine != "sqlite" && a.Engine != "redis" && !backupDBName.MatchString(a.DatabaseName) {
		return "", invalidBackup("Invalid recovery database name.")
	}
	name := "gopulse-restore-test-" + runID
	prefix := "work=" + shellQuote(work) + "\ntest_container=" + shellQuote(name) + "\n"
	cleanup := "trap 'docker rm --force --volumes \"$test_container\" >/dev/null 2>&1 || true' EXIT HUP INT TERM\n"
	common := "docker run --detach --rm --pull=never --network=none --read-only --cap-drop=ALL --security-opt=no-new-privileges --restart=no --memory=" + fmt.Sprint(max(plan.TestMemoryMB, 128)) + "m --cpus=1 --pids-limit=256 --tmpfs=/tmp:rw,nosuid,nodev,mode=1777 --tmpfs=/run:rw,nosuid,nodev,mode=1777 --label=" + shellQuote("gopulse.remote-backup-test="+runID) + " --name=\"$test_container\" "
	// Every loop is bounded both locally and by the enclosing GNU timeout.
	ready := func(command string) string {
		return "ready=0; for attempt in $(seq 1 120); do if " + command + " >/dev/null 2>&1; then ready=1; break; fi; sleep 1; done; test \"$ready\" = 1\n"
	}
	db := shellQuote(a.DatabaseName)
	image := shellQuote(a.ImageID)
	switch a.Engine {
	case "postgres":
		script := prefix + cleanup + common + "--user=postgres -e POSTGRES_HOST_AUTH_METHOD=trust -e POSTGRES_USER=gopulse_restore -e POSTGRES_DB=" + db + " " + image + " >/dev/null\n"
		pg := "docker exec \"$test_container\" psql -X -v ON_ERROR_STOP=1 -U gopulse_restore -d " + db
		script += ready("docker exec \"$test_container\" pg_isready -U gopulse_restore -d " + db)
		script += "docker exec -i \"$test_container\" pg_restore --exit-on-error --no-owner --no-privileges -U gopulse_restore -d " + db + " < \"$work/database.dump\"\n"
		query := `SELECT format('SELECT count(*)::bigint FROM %I.%I;',schemaname,tablename) FROM pg_tables WHERE schemaname NOT IN ('pg_catalog','information_schema');`
		script += pg + " -tA -c " + shellQuote(query) + " > \"$work/checks.sql\"\n" + "docker exec -i \"$test_container\" psql -X -v ON_ERROR_STOP=1 -U gopulse_restore -d " + db + " -tA < \"$work/checks.sql\" > \"$work/counts.txt\"\n"
		script += `printf 'Verified PostgreSQL restore: %s tables read, %s rows.\n' "$(wc -l < "$work/checks.sql" | tr -d ' ')" "$(awk '{n+=$1}END{print n+0}' "$work/counts.txt")"`
		return script, nil
	case "mysql":
		password := "restore-" + runID
		script := prefix + cleanup + common + "--user=mysql -e MYSQL_ROOT_PASSWORD=" + shellQuote(password) + " -e MYSQL_DATABASE=" + db + " " + image + " >/dev/null\n"
		cli := "docker exec -e MYSQL_PWD=" + shellQuote(password) + " \"$test_container\" mysql --user=root --batch --skip-column-names"
		script += ready(cli + " --execute='SELECT 1;'")
		script += "docker exec -i -e MYSQL_PWD=" + shellQuote(password) + " \"$test_container\" mysql --user=root --binary-mode --database=" + db + " < \"$work/database.dump\"\n"
		query := `SELECT CONCAT('SELECT COUNT(*) FROM \x60',REPLACE(TABLE_SCHEMA,'\x60','\x60\x60'),'\x60.\x60',REPLACE(TABLE_NAME,'\x60','\x60\x60'),'\x60;') FROM information_schema.TABLES WHERE TABLE_SCHEMA=` + "'" + strings.ReplaceAll(a.DatabaseName, "'", "''") + "' AND TABLE_TYPE='BASE TABLE';"
		query = strings.ReplaceAll(query, `\x60`, "`")
		script += cli + " --execute=" + shellQuote(query) + " > \"$work/checks.sql\"\n" + "docker exec -i -e MYSQL_PWD=" + shellQuote(password) + " \"$test_container\" mysql --user=root --batch --skip-column-names < \"$work/checks.sql\" > \"$work/counts.txt\"\n"
		script += `printf 'Verified MySQL restore: %s tables read, %s rows.\n' "$(wc -l < "$work/checks.sql" | tr -d ' ')" "$(awk '{n+=$1}END{print n+0}' "$work/counts.txt")"`
		return script, nil
	case "mongodb":
		script := prefix + cleanup + common + "--user=mongodb " + image + " --bind_ip 127.0.0.1 --wiredTigerCacheSizeGB 0.25 >/dev/null\n"
		script += ready(`docker exec "$test_container" mongosh --quiet --eval 'quit(db.adminCommand({ping:1}).ok?0:1)'`)
		script += "docker exec -i \"$test_container\" mongorestore --archive --gzip --stopOnError --nsInclude=" + shellQuote(a.DatabaseName+".*") + " < \"$work/database.dump\"\n"
		nameJSON, _ := json.Marshal(a.DatabaseName)
		query := `const d=db.getSiblingDB(` + string(nameJSON) + `);let rows=0,n=0;for(const c of d.getCollectionInfos({type:'collection'})){const v=d.runCommand({validate:c.name,full:true});if(!v.ok||!v.valid)throw Error('collection validation failed');rows+=d.getCollection(c.name).countDocuments({});n++;}print('Verified MongoDB restore: '+n+' collections read, '+rows+' documents.');`
		script += "docker exec \"$test_container\" mongosh --quiet --eval " + shellQuote(query)
		return script, nil
	case "redis":
		script := prefix + cleanup + common + "--user=redis " + image + " redis-server --appendonly no --save '' --dir /data >/dev/null\n"
		script += ready(`docker exec "$test_container" redis-cli ping`)
		script += `docker exec -i "$test_container" sh -c 'umask 077; cat > /data/dump.rdb' < "$work/database.dump"` + "\ndocker restart \"$test_container\" >/dev/null\n"
		script += ready(`docker exec "$test_container" redis-cli ping`)
		query := `local n=0; for _,k in ipairs(redis.call('KEYS','*')) do redis.call('DUMP',k); n=n+1; end; return n`
		script += "rows=0; for db in $(seq 0 15); do n=$(docker exec \"$test_container\" redis-cli -n \"$db\" --raw EVAL " + shellQuote(query) + " 0); [[ \"$n\" =~ ^[0-9]+$ ]]; rows=$((rows+n)); done\nprintf 'Verified Redis restore: 16 databases read, %s keys.\\n' \"$rows\""
		return script, nil
	case "sqlite":
		script := prefix + `test "$(sqlite3 -bail "$work/database.dump" 'PRAGMA integrity_check;')" = ok` + "\n"
		query := `SELECT 'SELECT count(*) FROM "' || replace(name,'"','""') || '";' FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%';`
		script += "sqlite3 -bail \"$work/database.dump\" " + shellQuote(query) + " > \"$work/checks.sql\"\nsqlite3 -bail \"$work/database.dump\" < \"$work/checks.sql\" > \"$work/counts.txt\"\n"
		script += `printf 'Verified SQLite restore: integrity check passed, %s tables read, %s rows.\n' "$(wc -l < "$work/checks.sql" | tr -d ' ')" "$(awk '{n+=$1}END{print n+0}' "$work/counts.txt")"`
		return script, nil
	default:
		return "", invalidBackup("Unsupported restore-test engine.")
	}
}
