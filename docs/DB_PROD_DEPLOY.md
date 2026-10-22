# Content deploy to the production database

`cmd/seed` loads `seed/` into SQLite. It compares a SHA-256 of every object (world, quest, question, asset) with the stored `content_hash` and writes only what changed, so a re-run with no edits writes nothing (and sends nothing to Mongo). Status is kept for unchanged questions (an admin may have published one); changed content comes back with the seed status and a new version.

## Steps (wrapper: `scripts/db-prod-deploy.sh`, dry run by default)

1. Validate the seed (schema, curriculum links, the rules of `internal/content`). Any error stops before the database is touched.
2. Backup: `VACUUM INTO backups/pre-seed-<utc>.db` (consistent while the API runs) and, when `litestream` is installed, print the newest replica snapshot. `--no-backup` is refused when `APP_ENV=production`.
3. Open the database: pending migrations (`0008_assets`, `0009_content`, ...) apply, each in its own transaction.
4. Apply per kind, one write transaction each: misconceptions, assets (and vocab), worlds, quests, questions (batches of 200). Each changed question gets a `question_versions` row (actor `seed:<run_id>`), `question_audit` and `admin_audit` rows and Mongo outbox rows, all in the same transaction.
5. Post checks: `integrity_check`, `foreign_key_check`, worlds present; `--strict` also demands target_count-2 published per quest.
6. `seed_runs` records counts, git rev, files hash, backup path and status.

```
bash scripts/db-prod-deploy.sh                    # dry run on a COPY of the database, writes nothing
bash scripts/db-prod-deploy.sh --apply
bash scripts/db-prod-deploy.sh --rollback <run_id>
go run ./cmd/seed --db data/dev.db --dry-run --json
```

Other flags: `--only worlds,quests,questions,assets`, `--world <id>`, `--exclude _examples`, `--allow-retire` (without it, questions removed from the seed are only reported), `--strict`, `--backup-dir`, `--actor`.

Optional step in `deploy/vps/deploy.sh`: `SEED_CONTENT=1 bash deploy/vps/deploy.sh ubuntu@<ip>` ships the seed files and runs a dry run then the apply. The wrapper excludes the demo file `_examples.json`.

The running API sees new content within 30 s (its view of the bank refreshes) and at once for admin edits made in the same process. Worlds and quests are owned by the seed files: an admin edit of a world or quest is a hotfix that the next seed run overwrites if the file differs.

## Rollback

- Questions: `--rollback <run_id>` restores, for every question that run changed, the snapshot from before the run (as a new version, action `rollback`), and retires questions the run added. Rows are never deleted.
- Worlds, quests, assets and misconception rows have no history: restore the backup.
- Catastrophe: stop the API, copy `backups/pre-seed-<ts>.db` over the database (remove the `-wal` and `-shm` files), or `litestream restore -timestamp <ts> -o /var/lib/numerix/numerix.db /var/lib/numerix/numerix.db`, then start the API. Progress written after the backup is lost, so prefer `--rollback` when only content is wrong.
