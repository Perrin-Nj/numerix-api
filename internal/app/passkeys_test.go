package app

// Passkey tests with a software authenticator (a fake security key written below). It plays the browser's
// part of WebAuthn: it makes a P-256 key, builds the attestation and assertion bytes and signs them, so the
// real go-webauthn verifier runs end to end through the HTTP stack.
//
// Go for Spring people: this is like using a test double for a remote service, except the double speaks the
// real protocol, so the production verification code is the code under test.

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Perrin-Nj/numerix-api/internal/config"
)

const (
	pkTestOrigin = "http://localhost:3000" // dev default of config.WebAuthnOrigins
	pkTestRPID   = "localhost"             // dev default of config.WebAuthnRPID
)

// fakeKey is one software passkey.
type fakeKey struct {
	priv   *ecdsa.PrivateKey
	credID []byte
	count  uint32
	handle []byte // user handle, learned at registration
}

func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

// cbor helpers: only the few shapes WebAuthn needs (all lengths here are below 256).
func cborHead(major byte, n int) []byte {
	if n < 24 {
		return []byte{major<<5 | byte(n)}
	}
	return []byte{major<<5 | 24, byte(n)}
}
func cborText(s string) []byte  { return append(cborHead(3, len(s)), s...) }
func cborBytes(b []byte) []byte { return append(cborHead(2, len(b)), b...) }
func cborNeg(n int) []byte      { return cborHead(1, -1-n) } // n is negative and above -25

// coseKey encodes the public key as a COSE_Key for ES256: {1:2, 3:-7, -1:1, -2:x, -3:y}.
func (k *fakeKey) coseKey() []byte {
	point, err := k.priv.PublicKey.Bytes() // uncompressed point: 0x04, then X and Y of 32 bytes each
	if err != nil {
		panic(err) // a test helper: a P-256 key always encodes
	}
	out := cborHead(5, 5)
	out = append(out, 0x01, 0x02)     // kty: EC2
	out = append(out, 0x03)           // alg
	out = append(out, cborNeg(-7)...) // ES256
	out = append(out, cborNeg(-1)...) // crv
	out = append(out, 0x01)           // P-256
	out = append(out, cborNeg(-2)...) // x
	out = append(out, cborBytes(point[1:33])...)
	out = append(out, cborNeg(-3)...) // y
	out = append(out, cborBytes(point[33:65])...)
	return out
}

func clientData(typ, challenge, origin string) []byte {
	b, _ := json.Marshal(map[string]any{"type": typ, "challenge": challenge, "origin": origin})
	return b
}

// optionsOf digs publicKey.challenge (and user.id for creation) out of a begin answer.
func optionsOf(t *testing.T, rec *httptest.ResponseRecorder) (session, challenge string, userID []byte) {
	t.Helper()
	body := dvJSON(rec)
	opts, _ := body["options"].(map[string]any)
	pk, _ := opts["publicKey"].(map[string]any)
	if rec.Code != 200 || pk == nil {
		t.Fatalf("begin %d %s", rec.Code, rec.Body)
	}
	if u, ok := pk["user"].(map[string]any); ok {
		userID, _ = base64.RawURLEncoding.DecodeString(u["id"].(string))
	}
	return plStr(body, "session_id"), plStr(pk, "challenge"), userID
}

// register builds the credential JSON for a creation ceremony ("none" attestation).
func (k *fakeKey) register(challenge, origin string, userID []byte) map[string]any {
	if k.priv == nil { // one key per fake device, so a second register call is the same credential
		k.priv, _ = ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		k.credID = make([]byte, 32)
		_, _ = rand.Read(k.credID)
	}
	k.handle = userID
	rp := sha256.Sum256([]byte(pkTestRPID))
	auth := append([]byte{}, rp[:]...)
	auth = append(auth, 0x01|0x04|0x40|0x08|0x10) // UP, UV, AT, BE, BS
	auth = binary.BigEndian.AppendUint32(auth, k.count)
	auth = append(auth, make([]byte, 16)...) // AAGUID: all zeros
	auth = binary.BigEndian.AppendUint16(auth, uint16(len(k.credID)))
	auth = append(auth, k.credID...)
	auth = append(auth, k.coseKey()...)
	att := cborHead(5, 3)
	att = append(att, cborText("fmt")...)
	att = append(att, cborText("none")...)
	att = append(att, cborText("attStmt")...)
	att = append(att, cborHead(5, 0)...)
	att = append(att, cborText("authData")...)
	att = append(att, cborBytes(auth)...)
	return map[string]any{
		"id": b64(k.credID), "rawId": b64(k.credID), "type": "public-key",
		"response": map[string]any{
			"clientDataJSON":    b64(clientData("webauthn.create", challenge, origin)),
			"attestationObject": b64(att),
			"transports":        []string{"internal", "hybrid"},
		},
	}
}

// assert builds the credential JSON for a request ceremony, signing with the next counter value (or a fixed one).
func (k *fakeKey) assert(challenge, origin string, counter uint32) map[string]any {
	rp := sha256.Sum256([]byte(pkTestRPID))
	auth := append([]byte{}, rp[:]...)
	auth = append(auth, 0x01|0x04|0x08|0x10)
	auth = binary.BigEndian.AppendUint32(auth, counter)
	cd := clientData("webauthn.get", challenge, origin)
	h := sha256.Sum256(cd)
	digest := sha256.Sum256(append(append([]byte{}, auth...), h[:]...))
	sig, _ := ecdsa.SignASN1(rand.Reader, k.priv, digest[:])
	return map[string]any{
		"id": b64(k.credID), "rawId": b64(k.credID), "type": "public-key",
		"response": map[string]any{
			"clientDataJSON": b64(cd), "authenticatorData": b64(auth), "signature": b64(sig), "userHandle": b64(k.handle),
		},
	}
}

// pkRegister runs a full registration for a parent and returns the key and the stored passkey id.
func pkRegister(t *testing.T, h http.Handler, tok string, k *fakeKey) string {
	t.Helper()
	session, ch, uid := optionsOf(t, dvCall(t, h, "POST", "/v1/auth/passkeys/register/begin", tok, "", nil))
	rec := dvCall(t, h, "POST", "/v1/auth/passkeys/register/finish", tok, "", map[string]any{
		"session_id": session, "nickname": "Laptop", "credential": k.register(ch, pkTestOrigin, uid)})
	if rec.Code != 201 {
		t.Fatalf("register finish %d %s", rec.Code, rec.Body)
	}
	return plStr(dvJSON(rec), "id")
}

func TestPasskeyRegisterListLoginDelete(t *testing.T) {
	h, _ := newTestServer(t)
	tok, _ := dvRegister(t, h, "pk@example.com")
	k := &fakeKey{}
	id := pkRegister(t, h, tok, k)

	// list shows the stored metadata
	rec := dvCall(t, h, "GET", "/v1/account/passkeys", tok, "", nil)
	items, _ := dvJSON(rec)["items"].([]any)
	if rec.Code != 200 || len(items) != 1 {
		t.Fatalf("list %d %s", rec.Code, rec.Body)
	}
	pk := items[0].(map[string]any)
	if pk["nickname"] != "Laptop" || pk["backup_eligible"] != true || pk["backup_state"] != true || pk["id"] != id {
		t.Fatalf("passkey %v", pk)
	}

	// login with no username: 200, a session and a device cookie
	login := func(counter uint32, origin string) *httptest.ResponseRecorder {
		session, ch, _ := optionsOf(t, dvCall(t, h, "POST", "/v1/auth/passkeys/login/begin", "", "", nil))
		return dvCall(t, h, "POST", "/v1/auth/passkeys/login/finish", "", "", map[string]any{"session_id": session, "credential": k.assert(ch, origin, counter)})
	}
	rec = login(1, pkTestOrigin)
	if rec.Code != 200 || plStr(dvJSON(rec), "access_token") == "" || dvCookie(rec) == nil {
		t.Fatalf("passkey login %d %s", rec.Code, rec.Body)
	}
	if rec = login(2, pkTestOrigin); rec.Code != 200 {
		t.Fatalf("second login %d %s", rec.Code, rec.Body)
	}
	// replaying a counter that does not go up looks like a cloned key
	if rec = login(2, pkTestOrigin); rec.Code != 401 {
		t.Fatalf("counter replay %d", rec.Code)
	}
	// wrong origin (phishing site)
	if rec = login(3, "https://evil.example"); rec.Code != 401 {
		t.Fatalf("wrong origin %d", rec.Code)
	}
	// session ids are single use
	session, ch, _ := optionsOf(t, dvCall(t, h, "POST", "/v1/auth/passkeys/login/begin", "", "", nil))
	body := map[string]any{"session_id": session, "credential": k.assert(ch, pkTestOrigin, 10)}
	if rec = dvCall(t, h, "POST", "/v1/auth/passkeys/login/finish", "", "", body); rec.Code != 200 {
		t.Fatalf("login %d", rec.Code)
	}
	if rec = dvCall(t, h, "POST", "/v1/auth/passkeys/login/finish", "", "", body); rec.Code != 410 {
		t.Fatalf("session reuse %d", rec.Code)
	}

	// sign count and last use were stored
	rec = dvCall(t, h, "GET", "/v1/account/passkeys", tok, "", nil)
	pk = dvJSON(rec)["items"].([]any)[0].(map[string]any)
	if pk["last_used_at"] == nil {
		t.Fatalf("last_used_at not set: %v", pk)
	}

	// another parent cannot delete it; the owner can; then login fails
	tok2, _ := dvRegister(t, h, "pk2@example.com")
	if rec = dvCall(t, h, "DELETE", "/v1/account/passkeys/"+id, tok2, "", nil); rec.Code != 404 {
		t.Fatalf("foreign delete %d", rec.Code)
	}
	if rec = dvCall(t, h, "DELETE", "/v1/account/passkeys/"+id, tok, "", nil); rec.Code != 204 {
		t.Fatalf("delete %d", rec.Code)
	}
	if rec = login(20, pkTestOrigin); rec.Code != 401 {
		t.Fatalf("login after delete %d", rec.Code)
	}
}

func TestPasskeyRegistrationRules(t *testing.T) {
	h, srv := newTestServer(t)
	tok, _ := dvRegister(t, h, "rules@example.com")
	tok2, _ := dvRegister(t, h, "rules2@example.com")

	// registration needs a parent token
	if rec := dvCall(t, h, "POST", "/v1/auth/passkeys/register/begin", "", "", nil); rec.Code != 401 {
		t.Fatalf("anonymous begin %d", rec.Code)
	}
	// a session id begun by one parent cannot be finished by another
	session, ch, uid := optionsOf(t, dvCall(t, h, "POST", "/v1/auth/passkeys/register/begin", tok, "", nil))
	k := &fakeKey{}
	cred := k.register(ch, pkTestOrigin, uid)
	if rec := dvCall(t, h, "POST", "/v1/auth/passkeys/register/finish", tok2, "", map[string]any{"session_id": session, "credential": cred}); rec.Code != 410 {
		t.Fatalf("foreign session %d", rec.Code)
	}
	// the session was burnt by that attempt, so start over; a bad origin is refused
	session, ch, uid = optionsOf(t, dvCall(t, h, "POST", "/v1/auth/passkeys/register/begin", tok, "", nil))
	cred = k.register(ch, "https://evil.example", uid)
	if rec := dvCall(t, h, "POST", "/v1/auth/passkeys/register/finish", tok, "", map[string]any{"session_id": session, "credential": cred}); rec.Code != 422 {
		t.Fatalf("bad origin %d %s", rec.Code, rec.Body)
	}
	// an expired session answers 410
	session, ch, uid = optionsOf(t, dvCall(t, h, "POST", "/v1/auth/passkeys/register/begin", tok, "", nil))
	past := plTS(time.Now().Add(-time.Minute))
	if _, err := srv.Store.W.Exec(`UPDATE webauthn_sessions SET expires_at = ? WHERE id = ?`, past, session); err != nil {
		t.Fatal(err)
	}
	if rec := dvCall(t, h, "POST", "/v1/auth/passkeys/register/finish", tok, "", map[string]any{"session_id": session, "credential": k.register(ch, pkTestOrigin, uid)}); rec.Code != 410 {
		t.Fatalf("expired %d", rec.Code)
	}
	// the same authenticator cannot be added twice (excluded at begin, unique key as safety net)
	pkRegister(t, h, tok, k)
	session, ch, uid = optionsOf(t, dvCall(t, h, "POST", "/v1/auth/passkeys/register/begin", tok, "", nil))
	if rec := dvCall(t, h, "POST", "/v1/auth/passkeys/register/finish", tok, "", map[string]any{"session_id": session, "credential": k.register(ch, pkTestOrigin, uid)}); rec.Code != 409 {
		t.Fatalf("duplicate %d %s", rec.Code, rec.Body)
	}
}

func TestPasskeysOffWithoutRPID(t *testing.T) {
	h, srv := newTestServer(t)
	tok, _ := dvRegister(t, h, "off@example.com")
	srv.WebAuthn = nil // production default: no WEBAUTHN_RP_ID
	for _, c := range []struct{ path, bearer string }{
		{"/v1/auth/passkeys/register/begin", tok}, {"/v1/auth/passkeys/login/begin", ""},
	} {
		rec := dvCall(t, h, "POST", c.path, c.bearer, "", nil)
		if rec.Code != 503 || plStr(dvJSON(rec), "code") != "passkeys_unavailable" {
			t.Fatalf("%s: %d %s", c.path, rec.Code, rec.Body)
		}
	}
}

// The documented real-domain shape (deploy/oracle/env.example) must produce a working passkey instance, not a silent nil.
func TestPasskeysProductionShape(t *testing.T) {
	env := map[string]string{
		"APP_ENV": "production", "JWT_SECRET": strings.Repeat("a", 32), "ADMIN_ORIGIN": "https://admin.example.ca",
		"ALLOWED_ORIGINS": "https://app.example.ca", "WEBAUTHN_RP_ID": "app.example.ca", "WEBAUTHN_ORIGINS": "https://app.example.ca",
	}
	cfg, err := config.Load(func(k string) string { return env[k] })
	if err != nil {
		t.Fatal(err)
	}
	if newWebAuthn(cfg) == nil {
		t.Fatal("production passkey config produced no WebAuthn instance")
	}
}
