-- name: FindUser :one
SELECT * FROM users WHERE name = $1;
