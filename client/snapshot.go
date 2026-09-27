package client

import (
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
)

func (c *Client) TriggerSnapshotExecution(ctx context.Context, project, workflow, ref, commitSHA string, snapshot io.Reader) (Execution, error) {
	var out Execution
	pr, pw := io.Pipe()
	mw := multipart.NewWriter(pw)
	contentType := mw.FormDataContentType()

	go func() {
		var err error
		if workflow != "" {
			err = mw.WriteField("workflow", workflow)
		}
		if err == nil && ref != "" {
			err = mw.WriteField("ref", ref)
		}
		if err == nil && commitSHA != "" {
			err = mw.WriteField("commit_sha", commitSHA)
		}
		var part io.Writer
		if err == nil {
			part, err = mw.CreateFormFile("snapshot", "snapshot.tar.gz")
		}
		if err == nil {
			_, err = io.Copy(part, snapshot)
		}
		if cerr := mw.Close(); err == nil {
			err = cerr
		}
		pw.CloseWithError(err)
	}()

	req, err := c.newRequest(ctx, http.MethodPost, "/api/projects/"+project+"/executions", pr)
	if err != nil {
		return out, err
	}
	req.Header.Set("Content-Type", contentType)
	data, err := c.sendWith(c.upload, req)
	if err != nil {
		return out, err
	}
	return out, json.Unmarshal(data, &out)
}
