package client

import (
	"context"
	"net/http"
	"net/url"
	"time"
)

type CacheImage struct {
	Reference  string    `json:"reference"`
	Size       int64     `json:"size"`
	Created    time.Time `json:"created"`
	Containers int64     `json:"containers"`
}

type CacheVolume struct {
	Name    string    `json:"name"`
	Cache   string    `json:"cache"`
	Size    int64     `json:"size"`
	Created time.Time `json:"created"`
}

type Cache struct {
	Project   string        `json:"project"`
	Images    []CacheImage  `json:"images"`
	Volumes   []CacheVolume `json:"volumes"`
	TotalSize int64         `json:"total_size"`
}

func (c *Client) ProjectCache(ctx context.Context, project string) (Cache, error) {
	return getJSON[Cache](c, ctx, "/api/projects/"+project+"/cache")
}

func (c *Client) DeleteCacheImage(ctx context.Context, project, reference string) error {
	_, err := c.do(ctx, http.MethodDelete, "/api/projects/"+project+"/cache/images?reference="+url.QueryEscape(reference), nil)
	return err
}

func (c *Client) DeleteCacheVolume(ctx context.Context, project, name string) error {
	_, err := c.do(ctx, http.MethodDelete, "/api/projects/"+project+"/cache/volumes?name="+url.QueryEscape(name), nil)
	return err
}
