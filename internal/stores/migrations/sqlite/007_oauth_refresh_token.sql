-- +goose Up
ALTER TABLE users ADD COLUMN oauth_refresh_token TEXT NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN oauth_token_expires_at INTEGER;

-- +goose Down
ALTER TABLE users DROP COLUMN oauth_token_expires_at;
ALTER TABLE users DROP COLUMN oauth_refresh_token;
