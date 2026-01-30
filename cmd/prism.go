package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/hashicorp/go-version"
)

var AppVersion = "v0.0.0"

type GitHubRelease struct {
	TagName string `json:"tag_name"`
}

func CheckForUpdates() {
	repoOwner := "yourusername"
	repoName := "your-api-repo"
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", repoOwner, repoName)

	client := http.Client{
		Timeout: 2 * time.Second,
	}

	resp, err := client.Get(url)
	if err != nil {
		return
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		return
	}

	var release GitHubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return
	}

	current, err := version.NewVersion(AppVersion)
	if err != nil {
		return
	}

	latest, err := version.NewVersion(release.TagName)
	if err != nil {
		return
	}

	if current.LessThan(latest) {
		fmt.Println("---------------------------------------------------------")
		fmt.Printf("⚠️  WARNING: You are running an outdated version (%s).\n", AppVersion)
		fmt.Printf("   The latest version is %s.\n", release.TagName)
		fmt.Println("   Please pull the latest Docker image.")
		fmt.Println("---------------------------------------------------------")
	}
}
