// Command mock-scm serves the GitHub and GitLab fixtures that the github and
// gitlab integrations in deps/seed.sql sync from, behind the tokens they
// reference as GP_SECRET_MOCK_GITHUB and GP_SECRET_MOCK_GITLAB.
package main

import (
	"log"
	"net/http"
	"os"
	"time"

	"github.com/dvjn/goldenportal/internal/integration/scmfixture"
)

func main() {
	github, gitlab := os.Getenv("GP_SECRET_MOCK_GITHUB"), os.Getenv("GP_SECRET_MOCK_GITLAB")
	if github == "" || gitlab == "" {
		log.Fatal("set GP_SECRET_MOCK_GITHUB and GP_SECRET_MOCK_GITLAB")
	}
	addr := "localhost:" + os.Getenv("MOCK_SCM_PORT")
	log.Printf("mock GitHub at http://%s/github, GitLab at http://%s/gitlab/api/v4", addr, addr)
	server := &http.Server{Addr: addr, Handler: scmfixture.Handler(github, gitlab), ReadHeaderTimeout: 5 * time.Second}
	log.Fatal(server.ListenAndServe())
}
