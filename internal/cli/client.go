package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/dvjn/goldport/internal/definition"
)

type client struct {
	base  string
	token string
	http  *http.Client
}

func newClient(getenv func(string) string) (*client, error) {
	base, token := getenv("GOLDPORT_URL"), getenv("GOLDPORT_TOKEN")
	if base == "" || token == "" {
		return nil, errors.New("set GOLDPORT_URL to the portal's address and GOLDPORT_TOKEN to an API token")
	}

	return &client{base: strings.TrimSuffix(base, "/") + "/api/v1/", token: token, http: &http.Client{Timeout: time.Minute}}, nil
}

type apiError struct {
	Message  string               `json:"message"`
	Problems []definition.Problem `json:"problems"`
}

func (e apiError) Error() string {
	lines := []string{e.Message}
	for _, problem := range e.Problems {
		lines = append(lines, "  "+problem.String())
	}

	return strings.Join(lines, "\n")
}

// call sends the body as JSON and decodes the data of the response into
// result.
func (c *client) call(method, path string, body, result any) error {
	var payload io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return err
		}
		payload = bytes.NewReader(encoded)
	}
	request, err := http.NewRequest(method, c.base+path, payload)
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+c.token)
	request.Header.Set("Content-Type", "application/json")
	response, err := c.http.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	var envelope struct {
		Data  json.RawMessage `json:"data"`
		Error *apiError       `json:"error"`
	}
	if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
		return fmt.Errorf("the portal answered %s without a JSON body", response.Status)
	}
	if envelope.Error != nil {
		return envelope.Error
	}

	return json.Unmarshal(envelope.Data, result)
}

type plan struct {
	ID      string `json:"id"`
	Summary struct {
		Added   int `json:"added"`
		Changed int `json:"changed"`
		Removed int `json:"removed"`
	} `json:"summary"`
	Changes []definition.Change `json:"changes"`
	Impact  struct {
		DeletedEntities []struct {
			Blueprint   string `json:"blueprint"`
			Integration string `json:"integration"`
			Count       int    `json:"count"`
		} `json:"deleted_entities"`
		ClearedValues []fieldImpact `json:"cleared_values"`
		DroppedLinks  []fieldImpact `json:"dropped_links"`
	} `json:"impact"`
}

type fieldImpact struct {
	Blueprint string `json:"blueprint"`
	Name      string `json:"name"`
	Count     int    `json:"count"`
}

var actionSigns = map[definition.Action]string{
	definition.Added:   "+",
	definition.Changed: "~",
	definition.Removed: "-",
}

func (p plan) print(out io.Writer) {
	if len(p.Changes) == 0 {
		fmt.Fprintln(out, "No changes. The portal's definitions match the files.")
		return
	}
	fmt.Fprintf(out, "Plan: %d to add, %d to change, %d to remove.\n\n", p.Summary.Added, p.Summary.Changed, p.Summary.Removed)
	for _, change := range p.Changes {
		fmt.Fprintf(out, "%s %s %s\n", actionSigns[change.Action], change.Kind, change.Identifier)
		for _, field := range change.Fields {
			fmt.Fprintf(out, "    %s: %s -> %s\n", field.Path, orNone(field.Before), orNone(field.After))
		}
	}
	impacts := p.impactLines()
	if len(impacts) > 0 {
		fmt.Fprintln(out, "\nImpact on entities:")
		for _, line := range impacts {
			fmt.Fprintln(out, "  "+line)
		}
	}
	fmt.Fprintln(out)
}

func (p plan) impactLines() []string {
	var lines []string
	for _, group := range p.Impact.DeletedEntities {
		if group.Blueprint != "" {
			lines = append(lines, fmt.Sprintf("%d entities of blueprint %s deleted", group.Count, group.Blueprint))
		} else {
			lines = append(lines, fmt.Sprintf("%d entities of integration %s deleted", group.Count, group.Integration))
		}
	}
	for _, field := range p.Impact.ClearedValues {
		lines = append(lines, fmt.Sprintf("%d values of %s.%s cleared", field.Count, field.Blueprint, field.Name))
	}
	for _, field := range p.Impact.DroppedLinks {
		lines = append(lines, fmt.Sprintf("%d entities lose their %s.%s links", field.Count, field.Blueprint, field.Name))
	}

	return lines
}

func orNone(value json.RawMessage) string {
	if len(value) == 0 {
		return "(none)"
	}

	return string(value)
}
