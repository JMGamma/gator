-- name: MarkFetched :exec
UPDATE feeds SET last_fetched = NOW(), updated_at = NOW() WHERE id = $1;
