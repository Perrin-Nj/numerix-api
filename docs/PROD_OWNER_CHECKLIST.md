# Numerix go-live: owner checklist

Tick in order. Full detail and "what you should see" after each step: `PROD_GO_LIVE.md` (section numbers below point there).
RULE: never paste a password, key or token into chat. Secrets go only into the named GitHub secret or the named file on the server.

## A. Decisions (write the answers down)

- [ ] D1 Approval gate (section 8.6). GitHub only allows "Required reviewers" on PUBLIC repositories on Free/Pro/Team. Pick one:
  - [ ] make both repos public, or
  - [ ] pay for a GitHub plan that supports it on private repos (check the plan page first), or
  - [ ] ask Claude to build the weaker typed-confirmation fallback (you stay the only approver).
- [ ] D2 Production host (section 5): OVH VPS in Quebec (about 7 USD per month in total, recommended) or Oracle Always Free (0 USD, can be reclaimed as idle).
- [ ] D3 Email (section 10): no email is sent today, so no password reset by email. Accept a manual support path for launch, or ask for it to be built.
- [ ] D4 Story writer (Anthropic): leave OFF at launch.
- [ ] D5 MongoDB Atlas: leave OFF at launch.

## B. Accounts and purchases (you, by hand)

- [ ] B1 Buy the domain (Porkbun `.ca`, about 9 USD per year, WHOIS privacy free). Section 6.
- [ ] B2 Create the host account. Oracle: pick home region Canada Southeast (Montreal). OVH: VPS-1, Beauharnois (Quebec). Read the order screen for currency and commitment BEFORE paying.
- [ ] B3 Create a bucket for backups (Oracle Object Storage) and an access key pair for it (Oracle: Profile, Customer Secret Keys). Optional second copy: Backblaze B2 bucket and application key.
- [ ] B4 Pick a free uptime monitor and READ its terms for commercial use (UptimeRobot, Better Stack or Healthchecks.io).
- [ ] B5 Delete or disconnect the old Vercel project (Vercel, Project, Settings, Git, Disconnect) so a push to `main` does not deploy there.

## C. What to send me in chat (NOT secrets)

- [ ] The domain name.
- [ ] Each VM public IP and CPU type (`arm64` for Oracle Ampere, `amd64` for most others).
- [ ] Oracle namespace and region (Profile, Tenancy), and the bucket names.
- [ ] Your D1, D2, D3 answers.
- [ ] Confirmation when DNS records, GitHub environments and secrets exist (names only).

## D. DNS (registrar, six A records, section 6)

- [ ] `app`, `admin`, `api` point to the PRODUCTION IP.
- [ ] `staging-app`, `staging-admin`, `staging-api` point to the STAGING IP.
- [ ] Check: `nslookup app.YOURDOMAIN` shows the production IP.

## E. GitHub (both repos: numerix-api and numerix-web)

- [ ] Settings, Environments: create `staging` (no rules) and `production` (Required reviewers: you).
- [ ] Repository variable `PROD_APPROVAL_CONFIRMED` = `yes` (only after production has Required reviewers).
- [ ] Per environment, secrets: `DEPLOY_SSH_KEY` (private key of that environment's deploy key), `DEPLOY_KNOWN_HOSTS` (`ssh-keyscan -t ed25519 VMIP` output, fingerprint checked against the VM).
- [ ] Per environment, variables: `DEPLOY_HOST`, `DEPLOY_USER` (= `deploy`), `TARGET_ARCH` (API repo), `API_URL` (API repo), `WEB_URL`, `ADMIN_URL` (web repo).
- [ ] Staging only, optional secret `SMOKE_ADMIN_API_TOKEN`. NEVER on production.
- [ ] If repos are private: secrets `WEB_REPO_TOKEN` and `API_REPO_TOKEN` (fine-grained, read-only, one per other repo).

## F. Deploy keys (your PC, section 17 step R4)

- [ ] `ssh-keygen -t ed25519 -f nx_deploy_staging -C numerix-staging-deploy -N ""` and the same for `nx_deploy_production`.
- [ ] Paste each PRIVATE file into the matching GitHub secret `DEPLOY_SSH_KEY`, then delete the private files from your PC. The `.pub` files go to the servers.

## G. Servers (section 17, R1 to R8)

For each VM (staging first):
- [ ] Create it: Ubuntu 24.04, your SSH public key.
- [ ] Open TCP 22, 80, 443 in the cloud firewall.
- [ ] Copy `deploy/oracle` and the matching `.pub`, run `setup.sh` with your three host names. Expect "setup done".
- [ ] Fill `/etc/numerix/api.env` (JWT_SECRET, ADMIN_API_TOKEN, origins, passkey lines), `/etc/numerix/admin.env` (same ADMIN_API_TOKEN, long ADMIN_PASSWORD, ADMIN_SESSION_SECRET), `/etc/litestream.yml`, `/etc/litestream.env`. Generate secrets with `openssl rand -hex 32` on the VM. Different values on staging and production.
- [ ] Copy the Discover media (200 files) once: `/var/lib/numerix/feed-media`.

## H. Rehearsal on staging (do not skip, section 17 part S)

- [ ] Merge to `main` (only after QA sign-off), watch `deploy-staging` finish green with `SMOKE PASSED`.
- [ ] Load the Discover items on staging.
- [ ] Use the staging site yourself: register, add a child, play, Discover videos, passkey, admin login.
- [ ] Load test on staging: zero 5xx, answer p95 under 400 ms.
- [ ] Restore drill: backup restores, integrity `ok`, about 1229 questions.
- [ ] Rollback drill: run `rollback`, then deploy forward again.

## I. Production (section 17 part P)

- [ ] Tag the API `v1.0.0`, press Approve, expect `SMOKE PASSED`.
- [ ] Run `db-content` dry run, read it, then apply.
- [ ] Load the Discover items on production.
- [ ] Tag the web repo `v1.0.0`, press Approve.
- [ ] Do the 10-minute manual smoke checklist (section 18) with a test parent, then delete it.
- [ ] Restore drill on production.
- [ ] Switch on monitoring; stop the web service for a minute to prove an alert arrives.
- [ ] Watch the first 24 hours (section 19) before announcing.

## J. Legal (before real families, section 13)

- [ ] Name the person in charge of personal information; publish their contact.
- [ ] Privacy policy and consent text in French and English (lawyer).
- [ ] Parental consent flow reviewed for children under 14 (lawyer).
- [ ] Privacy impact assessment, including the backup copy location.
- [ ] Confidentiality incident plan and register (lawyer).
- [ ] Terms of use and payments set up before charging anyone.
- [ ] Quebec native speaker review of the French script and French screens.

## K. Content you still owe the product

- [ ] Human voice recordings in fr-CA and en-CA (admin page `/voice`). Until then the app shows captions with no voice.
