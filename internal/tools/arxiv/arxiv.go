package arxiv

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Paper struct {
	ID        string
	Title     string
	Summary   string
	Authors   []string
	Published time.Time
	Updated   time.Time
	PDFURL    string
}

type Client struct {
	BaseURL string
	HTTP    *http.Client
}

func (c *Client) Search(ctx context.Context, query string, maxResults int) ([]Paper, error) {
	if strings.TrimSpace(query) == "" {
		return nil, nil
	}
	if maxResults <= 0 {
		maxResults = 5
	}

	base := strings.TrimRight(c.BaseURL, "/")
	if base == "" {
		base = "https://export.arxiv.org/api/query"
	}
	hc := c.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Second}
	}

	u, err := url.Parse(base)
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("search_query", "all:"+query)
	q.Set("start", "0")
	q.Set("max_results", fmt.Sprintf("%d", maxResults))
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "mingkunsearch/phase0")

	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}

	var feed atomFeed
	if err := xml.Unmarshal(body, &feed); err != nil {
		return nil, err
	}

	out := make([]Paper, 0, len(feed.Entries))
	for _, e := range feed.Entries {
		p := Paper{
			ID:      strings.TrimSpace(e.ID),
			Title:   normalizeSpace(e.Title),
			Summary: normalizeSpace(e.Summary),
		}
		for _, a := range e.Authors {
			if s := strings.TrimSpace(a.Name); s != "" {
				p.Authors = append(p.Authors, s)
			}
		}
		if t, err := time.Parse(time.RFC3339, strings.TrimSpace(e.Published)); err == nil {
			p.Published = t
		}
		if t, err := time.Parse(time.RFC3339, strings.TrimSpace(e.Updated)); err == nil {
			p.Updated = t
		}
		for _, l := range e.Links {
			if strings.EqualFold(l.Title, "pdf") || strings.Contains(strings.ToLower(l.Type), "pdf") {
				p.PDFURL = strings.TrimSpace(l.Href)
				break
			}
		}
		out = append(out, p)
	}
	return out, nil
}

type atomFeed struct {
	Entries []atomEntry `xml:"entry"`
}

type atomEntry struct {
	ID        string       `xml:"id"`
	Title     string       `xml:"title"`
	Summary   string       `xml:"summary"`
	Published string       `xml:"published"`
	Updated   string       `xml:"updated"`
	Authors   []atomAuthor `xml:"author"`
	Links     []atomLink   `xml:"link"`
}

type atomAuthor struct {
	Name string `xml:"name"`
}

type atomLink struct {
	Href  string `xml:"href,attr"`
	Title string `xml:"title,attr"`
	Type  string `xml:"type,attr"`
}

func normalizeSpace(s string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(s)), " ")
}
