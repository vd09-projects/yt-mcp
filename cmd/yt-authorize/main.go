// yt-authorize performs the one-time 3-legged OAuth2 consent grant for ONE
// channel (spec §5.1: one refresh token per channel, obtained via a one-time
// consent grant) and writes the token to that channel's token_file — the path
// configured in config.json (GOOGLE_APPLICATION_CREDENTIALS-style). No manual
// copy-paste into an env var. With neither --channel nor --out it falls back to
// printing the token.
//
// Usage:
//
//	yt-authorize --config config.json --channel main
//	yt-authorize --client-id ... --client-secret ... --out state/main.token.json
//
// Run it once per channel. Sign in with the Google account that manages the
// target channel and, if prompted, pick the correct brand/channel account —
// the tool prints which channel the token actually controls so tokens don't
// end up under the wrong alias.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/option"
	"google.golang.org/api/youtube/v3"

	"yt-mcp/internal/config"
	"yt-mcp/internal/uploader"
)

func main() {
	log.SetFlags(0)

	cfgPath := flag.String("config", "", "optional config.json to read the OAuth client credentials from")
	clientID := flag.String("client-id", "", "OAuth client id (or set YT_CLIENT_ID / --env-file)")
	clientSecret := flag.String("client-secret", "", "OAuth client secret (or set YT_CLIENT_SECRET / --env-file)")
	extraScopes := flag.String("scopes", "", "comma-separated EXTRA OAuth scopes to request beyond the upload defaults, e.g. https://www.googleapis.com/auth/yt-analytics.readonly for a future analytics server. Prefer minting a SEPARATE token per capability (least privilege) over one token with every scope")
	envFile := flag.String("env-file", "", "optional dotenv file of KEY=VALUE secrets, loaded before credentials resolve; file values override the shell environment")
	channel := flag.String("channel", "", "channel alias to write the token for; its token_file path is resolved from --config. The token JSON is written there directly (no copy-paste)")
	out := flag.String("out", "", "explicit path to write the token JSON to, overriding the --channel/--config-resolved path")
	flag.Parse()

	// Load the env file (if any) first, so both config.json ${VAR} expansion and
	// the os.Getenv fallback below see its values. File-wins — see LoadEnvFile.
	if *envFile != "" {
		if err := config.LoadEnvFile(*envFile); err != nil {
			log.Fatal(err)
		}
	}

	var oc *config.OAuthClient
	if *cfgPath != "" {
		// OAuth-only load: on first run the per-channel refresh tokens don't
		// exist yet, so full config validation would (correctly) fail.
		loaded, err := config.LoadOAuthOnly(*cfgPath)
		if err != nil {
			log.Fatal(err)
		}
		oc = loaded
	}

	id, secret := resolveCreds(*clientID, *clientSecret, oc)
	if id == "" || secret == "" {
		log.Fatal("client credentials required: pass --client-id/--client-secret, --config, --env-file, or set YT_CLIENT_ID/YT_CLIENT_SECRET")
	}

	// Resolve where to write the token. --out wins; otherwise --channel resolves
	// the path from the config's token_file field. If neither is given we fall
	// back to printing the token (legacy behavior) with a nudge.
	outPath := *out
	if outPath == "" && *channel != "" {
		if *cfgPath == "" {
			log.Fatal("--channel requires --config (the token_file path is read from it); or pass --out explicitly")
		}
		p, err := config.ResolveTokenFilePath(*cfgPath, *channel)
		if err != nil {
			log.Fatal(err)
		}
		outPath = p
	}

	// Loopback redirect on a random port — allowed for "Desktop app" OAuth
	// clients without pre-registering the port.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		log.Fatalf("listen on loopback: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port

	scopes := append([]string{}, uploader.Scopes...)
	if *extraScopes != "" {
		seen := map[string]bool{}
		for _, s := range scopes {
			seen[s] = true
		}
		for _, s := range strings.Split(*extraScopes, ",") {
			s = strings.TrimSpace(s)
			if s != "" && !seen[s] {
				scopes = append(scopes, s)
				seen[s] = true
			}
		}
		log.Printf("requesting %d scope(s): %s", len(scopes), strings.Join(scopes, " "))
	}

	oauthCfg := &oauth2.Config{
		ClientID:     id,
		ClientSecret: secret,
		Endpoint:     google.Endpoint,
		RedirectURL:  fmt.Sprintf("http://127.0.0.1:%d/callback", port),
		Scopes:       scopes,
	}

	stateBytes := make([]byte, 16)
	if _, err := rand.Read(stateBytes); err != nil {
		log.Fatalf("generate state: %v", err)
	}
	state := hex.EncodeToString(stateBytes)

	// AccessTypeOffline + prompt=consent guarantees a refresh token is
	// issued even if this client has been consented before.
	authURL := oauthCfg.AuthCodeURL(state,
		oauth2.AccessTypeOffline,
		oauth2.SetAuthURLParam("prompt", "consent"))

	fmt.Println("Open this URL in a browser and complete the consent flow.")
	fmt.Println("IMPORTANT: choose the Google account / brand account that owns the TARGET channel.")
	fmt.Println()
	fmt.Println(authURL)
	fmt.Println()

	codeCh := make(chan string, 1)
	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("state") != state {
			http.Error(w, "state mismatch", http.StatusBadRequest)
			return
		}
		if e := r.URL.Query().Get("error"); e != "" {
			http.Error(w, "consent error: "+e, http.StatusBadRequest)
			codeCh <- ""
			return
		}
		fmt.Fprintln(w, "Authorization received — you can close this tab and return to the terminal.")
		codeCh <- r.URL.Query().Get("code")
	})
	srv := &http.Server{Handler: mux}
	go srv.Serve(ln)

	var code string
	select {
	case code = <-codeCh:
	case <-time.After(10 * time.Minute):
		log.Fatal("timed out waiting for the OAuth callback")
	}
	if code == "" {
		log.Fatal("consent was denied or failed")
	}

	ctx := context.Background()
	tok, err := oauthCfg.Exchange(ctx, code)
	if err != nil {
		log.Fatalf("authorization code exchange failed: %v", err)
	}
	if tok.RefreshToken == "" {
		log.Fatal("no refresh token was returned; retry the flow (prompt=consent is forced, so this is unexpected)")
	}

	// Show which channel this token actually controls, so it doesn't get
	// pasted under the wrong alias in the config.
	svc, err := youtube.NewService(ctx, option.WithTokenSource(oauthCfg.TokenSource(ctx, tok)))
	if err != nil {
		log.Fatalf("build youtube client: %v", err)
	}
	resp, err := svc.Channels.List([]string{"snippet"}).Mine(true).Do()
	if err != nil {
		log.Fatalf("channels.list failed (token was issued, but verification failed): %v", err)
	}
	var channelID, channelTitle string
	if len(resp.Items) > 0 {
		channelID = resp.Items[0].Id
		channelTitle = resp.Items[0].Snippet.Title
		fmt.Printf("\nAuthorized channel: %s (id: %s)\n", channelTitle, channelID)
	} else {
		fmt.Println("\nToken issued, but no YouTube channel is attached to this account.")
	}

	tf := &config.TokenFile{
		RefreshToken: tok.RefreshToken,
		ChannelID:    channelID,
		ChannelTitle: channelTitle,
		ObtainedAt:   config.NowRFC3339(),
		Scopes:       scopes,
	}

	if outPath != "" {
		if err := config.WriteTokenFile(outPath, tf); err != nil {
			// The token IS valid — surface it so a write failure isn't a total
			// loss and the operator can place it manually.
			log.Printf("WARNING: token was issued but writing %s failed: %v", outPath, err)
			fmt.Println("\nRefresh token (write failed — store it manually):")
			fmt.Println()
			fmt.Println(tok.RefreshToken)
		} else {
			fmt.Printf("\nWrote token file: %s\n", outPath)
		}
	} else {
		fmt.Println("\nNo --channel/--out given, so printing the refresh token. Re-run with")
		fmt.Println("--channel <alias> --config config.json to write it to the channel's token_file directly.")
		fmt.Println()
		fmt.Println(tok.RefreshToken)
	}

	fmt.Println()
	fmt.Println("Reminder (spec §5.1): while the OAuth consent screen is in \"Testing\" publishing status,")
	fmt.Println("this token expires in ~7 days and this flow must be re-run. Complete Google app")
	fmt.Println("verification (\"In production\") for long-lived tokens.")

	shutdownCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
}

// resolveCreds picks the OAuth client id/secret by precedence, highest first:
//
//  1. explicit --client-id / --client-secret flags (flagID, flagSecret)
//  2. the --config oauth block (oc, nil when --config was not passed)
//  3. the YT_CLIENT_ID / YT_CLIENT_SECRET environment variables
//
// Because --env-file is loaded via godotenv.Overload before this runs, the
// env fallback (3) already reflects any env-file values, and an env-file entry
// beats the ambient shell — while an explicit flag or config value still wins
// over the file. id and secret are resolved independently, so a flag can supply
// one and config/env the other.
func resolveCreds(flagID, flagSecret string, oc *config.OAuthClient) (id, secret string) {
	id, secret = flagID, flagSecret
	if oc != nil {
		if id == "" {
			id = oc.ClientID
		}
		if secret == "" {
			secret = oc.ClientSecret
		}
	}
	if id == "" {
		id = os.Getenv("YT_CLIENT_ID")
	}
	if secret == "" {
		secret = os.Getenv("YT_CLIENT_SECRET")
	}
	return id, secret
}
