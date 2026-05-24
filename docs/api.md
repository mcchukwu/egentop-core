# Egentop API v1

Base URL:

/v1


# Authentication

Authentication uses:

- JWT access tokens
- HttpOnly refresh token cookies

Protected endpoints require:

Authorization: Bearer <access_token>


# Success Response Format

```json
{
  "success": true,
  "message": "resource fetched",
  "data": {}
}
```


# Error Response Format

```json
{
  "success": false,
  "error": {
    "code": "forbidden",
    "message": "forbidden"
  }
}
```


# Error Codes

| Code | Meaning |
|---|---|
| unauthorized | authentication required |
| forbidden | insufficient permissions |
| invalid_credentials | invalid login |
| validation_failed | invalid request input |
| invalid_json | malformed JSON |
| organization_not_found | organization missing |
| invalid_role | unsupported role |
| internal_server_error | unexpected server error |



# Authentication Endpoints

## POST /auth/register

Create user account.

### Request

```json
{
  "email": "test@example.com",
  "password": "password123",
  "first_name": "Miracle"
}
```

### Response

```json
{
  "success": true,
  "message": "registration successful"
}
```


## POST /auth/login

Authenticate user.

### Request

```json
{
  "identifier": "test@example.com",
  "password": "password123"
}
```

### Response

```json
{
  "success": true,
  "message": "login successful",
  "data": {
    "access_token": "jwt"
  }
}
```

# Organization Endpoints

## POST /orgs

Create organization.

### Auth Required
Yes

### Roles
Authenticated users

### Request

```json
{
  "name": "Acme Inc",
  "slug": "acme"
}
```

### Response

```json
{
  "success": true,
  "message": "organization created",
  "data": {
    "organization_id": "uuid"
  }
}
```

# Membership Endpoints

## GET /orgs/{orgID}/members

List organization members.

### Auth Required
Yes

### Roles
admin, owner


## POST /orgs/{orgID}/members

Add member to organization.

### Roles
admin, owner

### Request

```json
{
  "user_id": "uuid",
  "role": "member"
}
```


## PATCH /orgs/{orgID}/members/{userID}

Update member role.

### Roles
owner


## DELETE /orgs/{orgID}/members/{userID}

Remove member.

### Roles
owner

# Membership Endpoints

## GET /orgs/{orgID}/members

List organization members.

### Auth Required
Yes

### Roles
admin, owner


## POST /orgs/{orgID}/members

Add member to organization.

### Roles
admin, owner

### Request

```json
{
  "user_id": "uuid",
  "role": "member"
}
```


## PATCH /orgs/{orgID}/members/{userID}

Update member role.

### Roles
owner


## DELETE /orgs/{orgID}/members/{userID}

Remove member.

### Roles


# Pagination Standard

List endpoints use:

?page=1&limit=20

Paginated responses:

```json
{
  "success": true,
  "message": "members fetched",
  "data": {
    "items": [],
    "pagination": {
      "page": 1,
      "limit": 20,
      "total": 100
    }
  }
}
```
owner


# Security

- JWT access tokens are short-lived
- Refresh tokens use HttpOnly cookies
- RBAC enforced per organization
- Sessions can be revoked
- Rate limiting enabled
- Audit logging enabled
