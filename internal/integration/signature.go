package integration

import (
	"crypto/hmac"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"hash"
	"net/http"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/dvjn/goldenportal/internal/configdoc"
)

// Signature is how a system signs what it sends: an HMAC of the body, in hex,
// in a header, maybe after a prefix such as "sha256=". Connectors for systems
// that sign hard-code it; the http connector reads it from config.
type Signature struct {
	Header    string
	Algorithm string
	Prefix    string
	Secret    HeaderValue
}

// signatureAlgorithms are the hashes systems sign with. SHA-1 is weak, but
// some systems still only send it.
var signatureAlgorithms = map[string]func() hash.Hash{"sha1": sha1.New, "sha256": sha256.New, "sha512": sha512.New}

func parseSignature(r *configdoc.Reader, node *yaml.Node, path string) *Signature {
	fields, ok := r.Fields(node, path, SignatureFields...)
	if !ok {
		return nil
	}
	s := &Signature{
		Header:    r.Text(fields["header"], path+".header"),
		Algorithm: r.Text(fields["algorithm"], path+".algorithm"),
		Prefix:    r.Text(fields["prefix"], path+".prefix"),
	}
	if !headerNamePattern.MatchString(s.Header) {
		r.Errorf(configdoc.OrNode(fields["header"], node), path+".header", "header must name the header the signature is in")
	}
	if _, ok := signatureAlgorithms[s.Algorithm]; !ok {
		algorithms := make([]string, 0, len(signatureAlgorithms))
		for name := range signatureAlgorithms {
			algorithms = append(algorithms, name)
		}
		slices.Sort(algorithms)
		r.Errorf(configdoc.OrNode(fields["algorithm"], node), path+".algorithm",
			"algorithm must be one of %s", strings.Join(algorithms, ", "))
	}
	if fields["secret"] == nil {
		r.Errorf(node, path+".secret", "add the secret it's signed with, as {env: %s...}", SecretEnvPrefix)
		return s
	}
	s.Secret, _ = readSecret(r, fields["secret"], path+".secret")

	return s
}

// Verify checks body was signed with the secret.
func (s Signature) Verify(header http.Header, body []byte) error {
	sent := header.Get(s.Header)
	if sent == "" {
		return SignatureError{"sign the request in the " + s.Header + " header, or send an ingestion token as \"Authorization: Bearer <token>\""}
	}
	secret := s.Secret.Resolve()
	newHash, ok := signatureAlgorithms[s.Algorithm]
	if secret == "" || !ok {
		return SignatureError{"the integration's signing secret isn't set on the server"}
	}
	got, err := hex.DecodeString(strings.TrimPrefix(sent, s.Prefix))
	if err != nil || !strings.HasPrefix(sent, s.Prefix) {
		return SignatureError{"the " + s.Header + " header isn't a signature this integration reads"}
	}
	mac := hmac.New(newHash, []byte(secret))
	mac.Write(body)
	if !hmac.Equal(got, mac.Sum(nil)) {
		return SignatureError{"the " + s.Header + " signature doesn't match the body"}
	}

	return nil
}

// SignatureError is why a signed request was refused.
type SignatureError struct{ Reason string }

func (e SignatureError) Error() string { return e.Reason }
