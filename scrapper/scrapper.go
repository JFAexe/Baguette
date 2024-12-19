package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"strconv"
	"time"
)

const (
	apiMaxBatch        = 100
	apiHost            = "https://api.vk.com"
	apiPathMethod      = "method"
	apiMethodGroupByID = "groups.getById"
	apiMethodWall      = "wall.get"
	apiQueryAPIVersion = "v"
	apiQueryAPIToken   = "access_token"
	apiQueryGroupID    = "group_id"
	apiQueryOwnerID    = "owner_id"
	apiQueryCount      = "count"
	apiQueryOffset     = "offset"
)

type (
	GroupsResponse struct {
		Response struct {
			Groups []struct {
				ID   int    `json:"id"`
				Name string `json:"name"`
			} `json:"groups"`
		}
	}
	PostsResponse struct {
		Response struct {
			Count int `json:"count"`
			Items []struct {
				Text string `json:"text"`
			} `json:"items"`
		}
	}
)

var (
	OutputFilePath string
	PostSeparator  string
	GroupTarget    string
	APIToken       string
	APIVersion     string
	RequestSleep   time.Duration
)

func main() {
	flag.StringVar(&OutputFilePath, "o", "raw.txt", "output file path")
	flag.StringVar(&PostSeparator, "r", "<BAGUETTE>", "raw posts separator")
	flag.StringVar(&GroupTarget, "g", "57536014", "vk group")
	flag.StringVar(&APIToken, "t", "", "api token")
	flag.StringVar(&APIVersion, "v", "5.199", "api version")
	flag.DurationVar(&RequestSleep, "s", time.Millisecond*100, "sleep between requests")
	flag.Parse()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, os.Kill)
	defer cancel()

	if err := run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		log.Fatal(err)
	}
}

func run(ctx context.Context) error {
	ownerID := "-" + GroupTarget

	groups, err := get[GroupsResponse](ctx, apiMethodGroupByID, apiQueryGroupID, GroupTarget, apiQueryOwnerID, ownerID)
	if err != nil {
		return err
	}

	group := groups.Response.Groups[0]

	if len(groups.Response.Groups) == 0 {
		return errors.New("group not found")
	}

	log.Printf("target group: %d %q", group.ID, group.Name)

	posts, err := get[PostsResponse](ctx, apiMethodWall, apiQueryOwnerID, ownerID)
	if err != nil {
		return err
	}

	total := posts.Response.Count

	log.Printf("posts count: %d", total)

	abs, err := filepath.Abs(OutputFilePath)
	if err != nil {
		return fmt.Errorf("failed to transform output file path to absolute: %w", err)
	}

	output, err := os.Create(abs)
	if err != nil {
		return fmt.Errorf("failed to create output file: %w", err)
	}
	defer output.Close()

	log.Printf("saving posts to %q", abs)

	writer := bufio.NewWriter(output)
	defer writer.Flush()

	log.Printf("requesting posts")

	var count int

	for offset := 0; offset < total; offset += apiMaxBatch {
		if ctx.Err() != nil {
			break
		}

		batch := min(apiMaxBatch, total-offset)

		posts, err = get[PostsResponse](
			ctx, apiMethodWall,
			apiQueryOwnerID, ownerID,
			apiQueryCount, strconv.Itoa(batch),
			apiQueryOffset, strconv.Itoa(offset),
		)
		if err != nil {
			break
		}

		for i := range min(len(posts.Response.Items), batch) {
			count++

			if text := posts.Response.Items[i].Text; text != "" {
				writer.WriteString(text)
				writer.WriteString("\n")
				writer.WriteString(PostSeparator)
				writer.WriteString("\n")
			}
		}

		log.Printf("processed %d/%d posts\r", count, total)

		time.Sleep(RequestSleep)
	}

	log.Printf("saved %d posts", count)

	return err
}

func get[T any](ctx context.Context, method string, kv ...string) (res T, err error) {
	link, err := buildURL(method, kv...)
	if err != nil {
		return res, fmt.Errorf("failed to build request url: %w", err)
	}

	rcx, cancel := context.WithCancel(ctx)
	defer cancel()

	req, err := http.NewRequestWithContext(rcx, http.MethodGet, link, http.NoBody)
	if err != nil {
		return res, fmt.Errorf("failed to created request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return res, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode > http.StatusNoContent {
		return res, fmt.Errorf("failed to make request: %s", resp.Status)
	}

	if err = json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return res, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return res, nil
}

func buildURL(method string, kv ...string) (string, error) {
	link, err := url.Parse(apiHost)
	if err != nil {
		return "", fmt.Errorf("failed to parse base link: %w", err)
	}

	link.Path = path.Join(apiPathMethod, method)
	link.RawQuery = buildQuery(kv...)

	return link.String(), nil
}

func buildQuery(kv ...string) string {
	query := make(url.Values)

	query.Add(apiQueryAPIVersion, APIVersion)
	query.Add(apiQueryAPIToken, APIToken)

	for i := 0; i < len(kv)-1; i += 2 {
		query.Add(kv[i], kv[i+1])
	}

	return query.Encode()
}
