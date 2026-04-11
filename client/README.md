# Admin Client for GistDB

Simple web based admin client for the Document Database Service using HTML and Vanilla JS. This is run separately from the main service and is intended to be a small utility to aid in local testing and development. Not meant to be public/Internet facing.

## Demo

https://demodb.scottw.info/

## Features

- View collections — Select a collection from the dropdown to list all documents it contains.
- View documents — Click any document in the list to view its full contents, including metadata (ID, collection, timestamps) and data payload.
- Edit documents — Modify the document's JSON data inline and click Save to write the changes back.
- Create documents — Use the New Document button to create a document in any collection. Specify the collection name and JSON data payload.
- Delete documents — Remove a document permanently from the collection via the Delete button on the document view.
- Clear DB cache — Flush the server-side in-memory and file cache via the Clear DB Cache button, forcing the next read to fetch fresh data from GitHub.

## Token for GistDB Service

When running the main.go application it will act as a proxy for the frontend application and create a bearer token each time automatically. The expiration for this is two minutes.
This requires the env var JWT_PRIVATE_KEY with the base64 encoded private key contents for the main service bearer token.

## Authentication

This comes with a login page and is HTTPS enabled (self-signed certificate). To add your user follow these steps:

```
cd client
go run . --add-user myuser
<follow prompt to set password>
```

User credentials are stored in credentials.json as bcrypt-hashed passwords with a cost factor of 12. Plaintext passwords are never written to disk.

  Each entry in the file looks like:

  [
    {"username": "alice", "hash": "$2a$12$..."},
    {"username": "bob",   "hash": "$2a$12$..."}
  ]

  The hash format is $2a$<cost>$<salt><hash> where the salt is randomly generated per user, meaning two users with the same password will produce different hashes.

  ▎ credentials.json is excluded from version control via .gitignore.

## TLS / HTTPS

By default the client generates a self-signed certificate and serves over HTTPS. Two environment variables change this behaviour:

| Variable | Effect |
|---|---|
| `NO_TLS=1` | Serve plain HTTP — no certificate generated or required. Intended for deployments where TLS is terminated upstream (e.g. a reverse proxy). |
| `TLS_CERT_FILE` + `TLS_KEY_FILE` | Load a real signed certificate from the given paths instead of generating a self-signed one. Both must be set together. |

If neither is set, a self-signed certificate is generated at startup and the service is available at `https://localhost:8081`. Browsers will warn about the certificate; run `caddy trust` (or equivalent) once to add it to your system trust store.

## Demo Mode

Set env var `DEMO_MODE=1` to enable a read-only demo made 

## Quickstart

```
export JWT_PRIVATE_KEY="$(base64 -i private.pem)"
export GISTDB_SERVICE_URL="<url of main gistdb service>"
cd client
go run main.go
view in browser at https://localhost:8081
```