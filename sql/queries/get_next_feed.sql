-- name: GetNextFeed :one
SELECT * FROM feeds ORDER BY last_fetched NULLS FIRST LIMIT 1;
