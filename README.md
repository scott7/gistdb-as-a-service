# GistDB as a Service

A lightweight document database service that uses GitHub Gists as a backend storage layer. Built with Go, it provides a RESTful API for managing collections and documents with JWT authentication.

## Notice

This is a personal and educational project only. This is not intended to be a fully functioning or secure DB service.

## Overview

GistDB transforms GitHub Gists into a simple document database, offering:

- **Collections-based storage**: Organize documents into logical collections
- **RESTful API**: Standard HTTP methods for CRUD operations
- **JWT Authentication**: Secure access with RSA-signed tokens
- **Caching layer**: In-memory and file based caching for improved performance
- **GitHub-backed**: All data persisted as GitHub Gists

## Features

- Create, read, update, and delete documents
- Collection-based organization
- JWT authentication with issuer/audience validation
- Automatic indexing of collections
- Local caching for faster reads

## Cache

There are multiple caches for this service:

All data is cached via write-through method with everything persisting in Github.

1. Database Cache: This is an in memory cache to store the contents of all documents in the database. This is also using a write-through file based cache that the service will fall back to if the contents are not found in the in-memory cache. If the in memory cache exceeds a certain size it is cleared. The file-based cache will persist until the /tmp files are cleared (i.e. app is redeployed)
2. Filename Cache: This is an in memory and file based cache to map unique document ID to github gist ID.
3. Index Cache

## Example

The documents stored in the gist are in json format and look like this:

{
    "gist_id": "387ab96d8ade7a3792dc0ffd377ef3d8",
    "name": "3382f3024d37168d.json",
    "content": {
        "collection": "new_two",
        "createdAt": "2025-11-30T22:48:31-05:00",
        "data": {
            "tags": [
                "a",
                "b"
            ],
            "title": "This is title"
        },
        "id": "3382f3024d37168d.json"
    }
}

The "data" field contains the values that the user sets. The filename of the gist is also the unique ID used by the service (in this case `3382f3024d37168d.json`.) The schema within the "data" field is agnostic for this service and can be whatever the calling service needs.


## Quickstart with Docker

### Prerequisites

- Docker installed
- GitHub Personal Access Token with `gist` scope
- RSA key pair for JWT authentication (public key)

### 1. Build the Docker image

```bash
docker build -t gistdb-service .
```

### 2. Run the container

```bash
docker run -p 8080:8080 \
  -e GITHUB_TOKEN="your_github_token_here" \
  -e JWT_PUBLIC_KEY="$(cat public.pem)" \
  gistdb-service
```

### 3. Test the API

```bash
# Create a document (requires valid JWT token)
curl -X POST http://localhost:8080/collections/users \
  -H "Authorization: Bearer YOUR_JWT_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"data": {"name": "Jane", "email": "jane@example.com"}}'

# Get a document
curl -X GET http://localhost:8080/collections/users/DOCUMENT_ID \
  -H "Authorization: Bearer YOUR_JWT_TOKEN"
```

## API Endpoints

| Method | Endpoint | Description |
|--------|----------|-------------|
| `POST` | `/collections/{collection}` | Create a new document |
| `GET` | `/collections/{collection}/{id}` | Retrieve a document |
| `PATCH` | `/collections/{collection}/{id}` | Update a document |
| `DELETE` | `/collections/{collection}/{id}` | Delete a document |

## Environment Variables

| Variable | Required | Description |
|----------|----------|-------------|
| `GITHUB_TOKEN` | Yes | GitHub Personal Access Token with `gist` scope |
| `JWT_PUBLIC_KEY` | Yes | RSA public key in PEM format for JWT verification |

## JWT Authentication

All API requests require a valid JWT token with:

- **Issuer** (`iss`): `node-api`
- **Audience** (`aud`): `go-db-service`
- **Signing method**: RSA (RS256)

Include the token in the Authorization header:
```
Authorization: Bearer <your_jwt_token>
```

## Development

### Running locally

```bash
# Set environment variables
export GITHUB_TOKEN="your_token"
export JWT_PUBLIC_KEY="$(cat public.pem)"

# Run the application
cd gistdb
go run main.go
```
