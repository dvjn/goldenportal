package oauth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// A client may name itself by an https URL instead of registering: the
// URL serves its metadata, as client ID metadata documents describe
// (draft-ietf-oauth-client-id-metadata-document). The portal fetches it
// when someone authorizes the client, so the name, logo and redirect URIs
// shown are what the client's own domain publishes.
const (
	metadataPrefix   = "https://"
	maxMetadataBytes = 5 << 10
	metadataTimeout  = 5 * time.Second
	// Documents are kept as long as their Cache-Control says, within a day,
	// or ten minutes when it says nothing.
	metadataDefaultTTL = 10 * time.Minute
	metadataMaxTTL     = 24 * time.Hour
)

// isMetadataClient tells a client ID that is a metadata document URL from
// one the portal issued, which never starts with https://.
func isMetadataClient(clientID string) bool { return strings.HasPrefix(clientID, metadataPrefix) }

// clientMetadata is what a client says about itself, by registering or in
// its metadata document.
type clientMetadata struct {
	ClientID                string   `json:"client_id"`
	RedirectURIs            []string `json:"redirect_uris"`
	ClientName              string   `json:"client_name"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
	GrantTypes              []string `json:"grant_types"`
	ResponseTypes           []string `json:"response_types"`
	LogoURI                 string   `json:"logo_uri"`
}

// check accepts a public client's metadata and returns the name to show
// for it, or the OAuth error code and why not.
func (m clientMetadata) check(defaultName string) (string, *problem) {
	if m.TokenEndpointAuthMethod != "" && m.TokenEndpointAuthMethod != "none" {
		return "", &problem{"invalid_client_metadata", "clients are public: token_endpoint_auth_method must be none"}
	}
	// Grant types and response types the portal doesn't offer are simply
	// never used, as long as the one it does is among them: clients list
	// what they use with every server.
	if len(m.GrantTypes) > 0 && !slices.Contains(m.GrantTypes, "authorization_code") {
		return "", &problem{"invalid_client_metadata", "grant_types must include authorization_code"}
	}
	if len(m.ResponseTypes) > 0 && !slices.Contains(m.ResponseTypes, "code") {
		return "", &problem{"invalid_client_metadata", "response_types must include code"}
	}
	if len(m.RedirectURIs) == 0 || len(m.RedirectURIs) > maxRedirectURIs {
		return "", &problem{"invalid_redirect_uri", "register between 1 and 10 redirect_uris"}
	}
	for _, uri := range m.RedirectURIs {
		if err := checkRedirectURI(uri); err != nil {
			return "", &problem{"invalid_redirect_uri", err.Error()}
		}
	}
	name := strings.TrimSpace(m.ClientName)
	if name == "" {
		name = defaultName
	}
	if len([]rune(name)) > maxClientName {
		name = string([]rune(name)[:maxClientName])
	}

	return name, nil
}

// checkClientIDURL accepts a metadata document URL as the draft requires:
// https, a path without dot segments, and no credentials or fragment.
func checkClientIDURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return nil, errors.New("the client ID must be an https URL")
	}
	if u.User != nil || u.Fragment != "" || strings.Contains(raw, "#") {
		return nil, errors.New("the client ID URL can't have credentials or a fragment")
	}
	if u.Path == "" {
		return nil, errors.New("the client ID URL needs a path")
	}
	for _, segment := range strings.Split(u.Path, "/") {
		if segment == "." || segment == ".." {
			return nil, errors.New("the client ID URL can't have . or .. in its path")
		}
	}

	return u, nil
}

// errBlockedAddress refuses a fetch that would reach the portal's own
// network rather than the internet.
var errBlockedAddress = errors.New("the address isn't public")

// newMetadataClient fetches metadata documents from public addresses only.
// The address is checked as it's dialed, after DNS, so a name can't
// resolve one way when checked and another when fetched. With
// allowLoopback, for a portal itself on this machine, documents may also
// be served from it.
func newMetadataClient(allowLoopback bool) *http.Client {
	dialer := &net.Dialer{
		Timeout: metadataTimeout,
		Control: func(_, address string, _ syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return err
			}
			ip, err := netip.ParseAddr(host)
			if err != nil || !fetchable(ip, allowLoopback) {
				return errBlockedAddress
			}
			return nil
		},
	}

	return &http.Client{
		Timeout: metadataTimeout,
		// A proxy would dial for us, past the address check.
		Transport: &http.Transport{
			Proxy:               nil,
			DialContext:         dialer.DialContext,
			TLSHandshakeTimeout: metadataTimeout,
			MaxIdleConns:        10,
			IdleConnTimeout:     time.Minute,
		},
		// The document must be at the client ID itself; a redirect answers
		// as the error it is.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

// specialPrefixes are special-use ranges (RFC 6890) the netip checks
// don't already cover.
var specialPrefixes = func() []netip.Prefix {
	var prefixes []netip.Prefix
	for _, p := range []string{
		"0.0.0.0/8", "100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24", "198.18.0.0/15",
		"198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4",
		"64:ff9b::/96", "64:ff9b:1::/48", "100::/64", "2001::/23", "2001:db8::/32", "2002::/16",
	} {
		prefixes = append(prefixes, netip.MustParsePrefix(p))
	}
	return prefixes
}()

func fetchable(ip netip.Addr, allowLoopback bool) bool {
	ip = ip.Unmap()
	if ip.IsLoopback() {
		return allowLoopback
	}
	if !ip.IsGlobalUnicast() || ip.IsPrivate() {
		return false
	}
	for _, p := range specialPrefixes {
		if p.Contains(ip) {
			return false
		}
	}

	return true
}

// fetched is what a client's metadata document says, checked, and how
// long it may be kept.
type fetched struct {
	name         string
	redirectURIs []string
	logoURI      string
	ttl          time.Duration
}

// fetchMetadata reads and checks a client's metadata document.
func (s Server) fetchMetadata(ctx context.Context, clientID string) (fetched, error) {
	u, err := checkClientIDURL(clientID)
	if err != nil {
		return fetched{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, clientID, nil)
	if err != nil {
		return fetched{}, err
	}
	req.Header.Set("Accept", "application/json")
	res, err := s.metadata.Do(req)
	if errors.Is(err, errBlockedAddress) {
		return fetched{}, fmt.Errorf("%s isn't a public address", u.Host)
	}
	if err != nil {
		return fetched{}, fmt.Errorf("%s couldn't be reached", u.Host)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fetched{}, fmt.Errorf("%s answered %d instead of the app's metadata", u.Host, res.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, maxMetadataBytes+1))
	if err != nil {
		return fetched{}, fmt.Errorf("%s couldn't be read", u.Host)
	}
	if len(body) > maxMetadataBytes {
		return fetched{}, errors.New("the app's metadata is over 5 KB")
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil {
		return fetched{}, errors.New("the app's metadata isn't a JSON object")
	}
	for _, secret := range []string{"client_secret", "client_secret_expires_at"} {
		if _, ok := fields[secret]; ok {
			return fetched{}, errors.New("the app's metadata can't hold a client secret")
		}
	}
	var m clientMetadata
	if err := json.NewDecoder(bytes.NewReader(body)).Decode(&m); err != nil {
		return fetched{}, errors.New("the app's metadata has a field of the wrong type")
	}
	if m.ClientID != clientID {
		return fetched{}, errors.New("the app's metadata names another client_id")
	}
	name, p := m.check(u.Hostname())
	if p != nil {
		return fetched{}, errors.New("the app's metadata is invalid: " + p.description)
	}

	return fetched{name, m.RedirectURIs, m.LogoURI, cacheTTL(res.Header.Get("Cache-Control"))}, nil
}

// cacheTTL is how long Cache-Control lets a document be kept, within a day.
func cacheTTL(header string) time.Duration {
	ttl := metadataDefaultTTL
	for _, directive := range strings.Split(header, ",") {
		key, value, _ := strings.Cut(strings.TrimSpace(strings.ToLower(directive)), "=")
		switch key {
		case "no-store", "no-cache":
			return 0
		case "max-age":
			seconds, err := strconv.Atoi(strings.Trim(value, `"`))
			if err != nil || seconds < 0 {
				return 0
			}
			ttl = time.Duration(seconds) * time.Second
		}
	}

	return min(ttl, metadataMaxTTL)
}

// client finds a client: one the portal registered, or one named by its
// metadata URL, fetched again once what was kept of it expires. A
// document that can't be fetched or used is never kept, and the request
// fails rather than trusting an older copy.
func (s Server) client(ctx context.Context, store Store, clientID string) (Client, error) {
	if !isMetadataClient(clientID) {
		return store.Client(ctx, clientID)
	}
	c, err := store.Client(ctx, clientID)
	switch {
	case err == nil && c.MetadataExpires != nil && time.Now().Before(*c.MetadataExpires):
		return c, nil
	case err != nil && !errors.Is(err, errNotFound):
		return Client{}, err
	}
	f, err := s.fetchMetadata(ctx, clientID)
	if err != nil {
		return Client{}, metadataError{err}
	}

	return store.SaveMetadataClient(ctx, Client{
		ClientID: clientID, Name: f.name, RedirectURIs: f.redirectURIs, Logo: s.fetchLogo(ctx, f.logoURI),
	}, time.Now().Add(f.ttl))
}

// metadataError is why a client's metadata document can't be used.
type metadataError struct{ err error }

func (e metadataError) Error() string { return e.err.Error() }
