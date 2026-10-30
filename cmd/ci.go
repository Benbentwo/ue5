package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/Benbentwo/ue5/pkg/ci"
	"github.com/Benbentwo/ue5/pkg/server"
	"github.com/spf13/cobra"
)

var (
	ciTokenFile string
	ciURL       string
	ciReq       ci.Request
	ciPlugins   []string
	ciJobsLimit int
	ciJobsGame  string
)

var ciCmd = &cobra.Command{
	Use:   "ci",
	Short: "Dispatch and inspect builds on this box's build service",
	Long: `The build service runs inside the ue5 daemon on a build box. A pipeline job
(or a person) dispatches a build with 'ue5 ci dispatch'; the daemon queues it,
syncs the checkout, runs the game repo's ci/<kind>.sh (or .ps1), and reports
the result as a commit status on Azure Repos and on the dashboard
(http://<box>.local:9516).

Turn a box on with "enabled": true in ~/.ue5/ci/config.json.`,
}

var ciDispatchCmd = &cobra.Command{
	Use:   "dispatch",
	Short: "Queue a build and return at once",
	Example: `  ue5 ci dispatch --game MyGame --kind compile --platform Mac \
    --repo "$(Build.Repository.Uri)" --sha "$(Build.SourceVersion)" \
    --plugin SharedPlugins=https://dev.azure.com/Org/Project/_git/SharedPlugins@<sha> \
    --source "azure:$(Build.BuildId)"`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ciReq.Plugins = map[string]ci.Pinned{}
		for _, p := range ciPlugins {
			name, rest, ok := strings.Cut(p, "=")
			at := strings.LastIndex(rest, "@")
			if !ok || at < 0 {
				return fmt.Errorf("--plugin %q must be Name=<repo url>@<sha>", p)
			}
			ciReq.Plugins[name] = ci.Pinned{Repo: rest[:at], SHA: rest[at+1:]}
		}
		if err := ciReq.Validate(); err != nil {
			return err
		}
		_ = server.EnsureDaemon()
		body, _ := json.Marshal(ciReq)
		var v ci.JobView
		if err := ciPost("/api/ci/jobs", body, &v); err != nil {
			return err
		}
		fmt.Printf("queued %s on %s: %s %s %s at %s (position %d)\n", v.ID, v.Box, v.Game, v.Kind, v.Platform, v.SHA[:9], v.Position)
		fmt.Printf("follow it at %s\n", v.URL)
		return nil
	},
}

var ciJobsCmd = &cobra.Command{
	Use:   "jobs",
	Short: "List this box's builds, newest first",
	RunE: func(cmd *cobra.Command, args []string) error {
		_ = server.EnsureDaemon()
		var out struct {
			Box  string       `json:"box"`
			Jobs []ci.JobView `json:"jobs"`
		}
		q := fmt.Sprintf("/api/ci/jobs?limit=%d", ciJobsLimit)
		if ciJobsGame != "" {
			q += "&game=" + ciJobsGame
		}
		if err := ciGet(q, &out); err != nil {
			return err
		}
		tw := tabwriter.NewWriter(os.Stdout, 2, 4, 2, ' ', 0)
		_, _ = fmt.Fprintln(tw, "ID\tGAME\tKIND\tPLATFORM\tCOMMIT\tSTATE\tTIME\tNOTE")
		for _, j := range out.Jobs {
			note := j.Message
			if len(j.Errors) > 0 {
				note = j.Errors[0]
			}
			if j.Position > 0 {
				note = fmt.Sprintf("#%d in line %s", j.Position, note)
			}
			_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n", j.ID, j.Game, j.Kind, j.Platform, j.SHA[:9], j.State,
				(time.Duration(j.DurationSec) * time.Second).String(), truncate(note, 80))
		}
		return tw.Flush()
	},
}

var ciCancelCmd = &cobra.Command{
	Use:   "cancel <job-id>",
	Short: "Cancel a queued or running build",
	Args:  cobra.ExactArgs(1),
	RunE:  func(cmd *cobra.Command, args []string) error { return ciAction(args[0], "cancel") },
}

var ciRetryCmd = &cobra.Command{
	Use:   "retry <job-id>",
	Short: "Queue a fresh copy of a build",
	Args:  cobra.ExactArgs(1),
	RunE:  func(cmd *cobra.Command, args []string) error { return ciAction(args[0], "retry") },
}

func ciAction(id, action string) error {
	_ = server.EnsureDaemon()
	var v ci.JobView
	if err := ciPost("/api/ci/jobs/"+id+"/"+action, nil, &v); err != nil {
		return err
	}
	fmt.Printf("%s: %s is %s\n", action, v.ID, v.State)
	return nil
}

func ciPost(path string, body []byte, out any) error {
	tok, err := ci.DispatchTokenAt(ci.TokenPath(server.HomeDir(), ciTokenFile))
	if err != nil {
		return fmt.Errorf("read the dispatch token: %w", err)
	}
	req, _ := http.NewRequest(http.MethodPost, strings.TrimRight(ciURL, "/")+path, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+tok)
	return ciDo(req, out)
}

func ciGet(path string, out any) error {
	req, _ := http.NewRequest(http.MethodGet, strings.TrimRight(ciURL, "/")+path, nil)
	return ciDo(req, out)
}

func ciDo(req *http.Request, out any) error {
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return fmt.Errorf("the build service at %s did not answer (is the ue5 daemon running?): %w", ciURL, err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode/100 != 2 {
		var e struct{ Error string }
		if json.Unmarshal(b, &e) == nil && e.Error != "" {
			return fmt.Errorf("build service: %s", e.Error)
		}
		return fmt.Errorf("build service: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	return json.Unmarshal(b, out)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

func init() {
	rootCmd.AddCommand(ciCmd)
	ciCmd.PersistentFlags().StringVar(&ciURL, "url", "http://localhost:9516", "build service to talk to")
	ciCmd.PersistentFlags().StringVar(&ciTokenFile, "token-file", "", "dispatch token file (default $UE5_CI_TOKEN_FILE, else ~/.ue5/ci/token)")
	f := ciDispatchCmd.Flags()
	f.StringVar(&ciReq.Game, "game", "", "game name; also its checkout folder and .uproject name")
	f.StringVar((*string)(&ciReq.Kind), "kind", "compile", "compile, package or steam")
	f.StringVar(&ciReq.Platform, "platform", "", "Win64, Mac, ...")
	f.StringVar(&ciReq.Repo, "repo", "", "game repo URL")
	f.StringVar(&ciReq.SHA, "sha", "", "game commit (40 hex)")
	f.StringVar(&ciReq.Branch, "branch", "", "branch name, for the record")
	f.StringArrayVar(&ciPlugins, "plugin", nil, "extra repo to pin under the plugins dir: Name=<repo url>@<sha> (repeatable)")
	f.StringVar(&ciReq.Source, "source", "manual", "who asked, e.g. azure:1612")
	f.StringVar(&ciReq.SourceURL, "source-url", "", "link back to the dispatching run")
	ciJobsCmd.Flags().IntVar(&ciJobsLimit, "limit", 20, "how many to show")
	ciJobsCmd.Flags().StringVar(&ciJobsGame, "game", "", "only this game")
	ciCmd.AddCommand(ciDispatchCmd, ciJobsCmd, ciCancelCmd, ciRetryCmd)
}

var ciDoctorRepo string

var ciDoctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Check this box's build-service credentials against Azure DevOps",
	Long: `Reports where the azure-pat secret comes from (never its value), whether Azure
DevOps accepts it, and whether git can read a repository with it the way the
build service does. Run it on the build box, as the user the daemon runs as.`,
	Example: `  ue5 ci doctor --repo https://dev.azure.com/MyOrg/MyProject/_git/MyGame`,
	RunE: func(cmd *cobra.Command, args []string) error {
		home := server.HomeDir()
		pat, src := ci.SecretWithSource(home, "azure-pat")
		ok := true
		if pat == "" {
			fmt.Println("azure-pat: NOT FOUND (env UE5_CI_AZURE_PAT, Keychain service ue5-ci / account azure-pat, or ~/.ue5/ci/secrets/azure-pat)")
			return fmt.Errorf("no azure-pat secret")
		}
		fmt.Printf("azure-pat: found in %s (%d characters)\n", src, len(pat))
		if strings.ContainsAny(pat, " '\"\t") {
			fmt.Println("  warning: the value contains spaces or quotes; it was probably stored with extra characters")
		}
		repo := ciDoctorRepo
		if repo == "" {
			fmt.Println("(pass --repo <azure repo url> to also test the organization and a git read)")
			return nil
		}
		az, isAzure := ci.ParseAzureRepo(repo)
		if !isAzure {
			return fmt.Errorf("%s is not an Azure Repos URL", repo)
		}
		if who, err := ci.CheckAuth(cmd.Context(), nil, az.Base, pat); err != nil {
			ok = false
			fmt.Printf("organization %s: REJECTED: %v\n", az.Base, err)
		} else {
			fmt.Printf("organization %s: accepted (signed in as %s)\n", az.Base, who)
		}
		w := ci.Workspace{PAT: pat}
		if out, err := w.LsRemote(cmd.Context(), repo); err != nil {
			ok = false
			fmt.Printf("git read %s: FAILED: %v\n%s\n", repo, err, strings.TrimSpace(out))
		} else {
			fmt.Printf("git read %s: ok (%s)\n", repo, strings.TrimSpace(out))
		}
		if !ok {
			fmt.Println("\nFix: create a PAT in that organization with Code (Read & write), then store it with:")
			fmt.Println("  security add-generic-password -U -s ue5-ci -a azure-pat -w '<PAT>'   # macOS (-U replaces)")
			fmt.Println("and restart the daemon: ue5 server stop && ue5 ci jobs")
			return fmt.Errorf("credentials not working")
		}
		return nil
	},
}

func init() {
	ciDoctorCmd.Flags().StringVar(&ciDoctorRepo, "repo", "", "an Azure Repos URL to test a git read against")
	ciCmd.AddCommand(ciDoctorCmd)
}
