# SEC-PERF-001 rollout notes

## Order
1. Merge PR (CI green). Run the deploy-prod workflow: it builds with Go 1.27.4 (setup-go reads the go.mod toolchain line).
2. Re-install the API unit on every existing VM (setup.sh only runs at bootstrap and the deploy user cannot touch systemd units).
   As root on the VM:
   `install -m 644 deploy/vps/numerix-api.service /etc/systemd/system/numerix-api.service && systemctl daemon-reload && systemctl restart numerix-api`
   Until then the API runs fine, only without GOMEMLIMIT=512MiB. Override per VM with GOMEMLIMIT in /etc/numerix/api.env.
3. Check: `systemctl show numerix-api -p Environment` shows GOMEMLIMIT, and /v1/health is ok.

## Rollback
Previous release: `numerix-deploy activate api <previous-id>` or the rollback workflow. To drop the memory cap, restore the old unit file and daemon-reload.

## HSTS duplicate (left as is)
Caddy sets `max-age=31536000`, the app (production) sets `max-age=31536000; includeSubDomains`. Browsers use the first header.
Dropping either changes whether includeSubDomains applies, and the app header is off in non-production, so it is not provably safe here. Decide separately.
