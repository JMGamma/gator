-- +goose Up
ALTER TABLE feeds ADD COLUMN last_fetched TIMESTAMPTZ;

-- +goose Down
DROP COLUMN last_fetched FROM feeds;