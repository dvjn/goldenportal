package integration

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/dvjn/goldenportal/internal/configdoc"
)

// What connectors for code hosts, such as GitLab, share besides calling
// their API and reading them whole: their config, their webhooks, and
// reading the objects they give.

// hookTimeout bounds setting up webhooks after a sync's reads, which has a
// moment of its own even when reading ran out of time.
const hookTimeout = 2 * time.Minute

// defaultHistoryDays keeps a month of what piles up, which a sync of a busy
// repository reads in a few pages, plus a request or so for each run.
const defaultHistoryDays = 30

func readHistoryDays(r *configdoc.Reader, fields map[string]*yaml.Node, path string) int {
	days := defaultHistoryDays
	if n := fields["historyDays"]; n != nil {
		if err := n.Decode(&days); err != nil || days < 1 {
			r.Errorf(n, path+".historyDays", "historyDays must be a whole number of days, at least 1")
		}
	}

	return days
}

// readNames reads a list of names, such as groups, each one valid says,
// refusing any listed already. listed are the names read so far, lowercase,
// since code hosts' names ignore case.
func readNames(r *configdoc.Reader, fields map[string]*yaml.Node, path, name string, listed map[string]bool,
	valid func(string) bool, want string,
) []string {
	var out []string
	r.List(fields[name], path+"."+name, func(i int, n *yaml.Node) {
		itemPath := fmt.Sprintf("%s.%s[%d]", path, name, i)
		value := r.Text(n, itemPath)
		switch {
		case !valid(value):
			r.Errorf(n, itemPath, "%q isn't %s", value, want)
		case listed[strings.ToLower(value)]:
			r.Errorf(n, itemPath, "%s is listed twice", value)
		default:
			listed[strings.ToLower(value)] = true
			out = append(out, value)
		}
	})

	return out
}

// hookURL is the ingest URL with a fingerprint of the secret, so a sync can
// tell from a webhook alone, which never shows its secret, whether it has
// the current one, as it won't once the server's key changes, and changes
// it only when it hasn't. The fingerprint is an HMAC, which says nothing of
// the secret.
func hookURL(ingestURL, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(ingestURL))

	return ingestURL + "?fingerprint=" + hex.EncodeToString(mac.Sum(nil))[:16]
}

// ingestPart is a webhook's URL without its query, which is how a connector
// finds its own.
func ingestPart(address string) string {
	before, _, _ := strings.Cut(address, "?")
	return before
}

func timeOf(object map[string]any, field string) time.Time {
	text, _ := object[field].(string)
	at, _ := time.Parse(time.RFC3339, text)

	return at
}

// idOf formats a numeric ID for a URL, where a float64's own format would
// use an exponent.
func idOf(value any) string {
	if n, ok := value.(float64); ok {
		return strconv.FormatFloat(n, 'f', 0, 64)
	}

	return ""
}

func objectOf(object map[string]any, key string) map[string]any {
	child, _ := object[key].(map[string]any)
	return child
}

func sentence(reason string) string { return strings.ToUpper(reason[:1]) + reason[1:] + "." }

// sentenceList joins words as a sentence would: a, b and c.
func sentenceList(words []string) string {
	if len(words) < 2 {
		return strings.Join(words, "")
	}

	return strings.Join(words[:len(words)-1], ", ") + " and " + words[len(words)-1]
}
