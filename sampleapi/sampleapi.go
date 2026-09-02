package sampleapi

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
)

// User becomes components/schemas/User in the emitted spec.
// The struct tags here ARE the contract — drift is detected against them.
type User struct {
	UserID string `json:"userId"`               // exposed as "userId"; renaming changes the contract
	Email  string `json:"email" format:"email"` // format:"email" is emitted as a schema constraint
}

// GetUserInput declares what the request accepts.
// path:"id" -> a required path parameter in OpenAPI.
type GetUserInput struct {
	ID string `path:"id" doc:"ID of the user to fetch"` // doc -> description (the prose aspect)
}

// GetUserOutput: the type in the Body field becomes the 200 response schema.
// huma convention: the top-level Body field is the response body.
type GetUserOutput struct {
	Body User
}

// New builds a fully-wired huma.API and returns it, so callers can extract
// its contract without knowing huma's setup details.
func New() huma.API {
	mux := http.NewServeMux()                         // underlying std-lib router
	config := huma.DefaultConfig("User API", "1.2.0") // title + version -> OpenAPI info block
	api := humago.New(mux, config)                    // bind huma to net/http via humago

	addRoutes(api)
	return api
}

// addRoutes registers operations. huma.Register reads the handler signature
// and struct tags to auto-generate the corresponding OpenAPI operation.
func addRoutes(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "get-user",     // OpenAPI operationId; a stable key for diffing
		Method:      http.MethodGet, // -> paths./users/{id}.get
		Path:        "/users/{id}",
		Summary:     "Fetch a single user", // prose (human aspect)
	}, func(ctx context.Context, in *GetUserInput) (*GetUserOutput, error) {
		// Business logic is irrelevant here — we only extract the *contract*,
		// so a stub return keeps the operation well-formed.
		return &GetUserOutput{Body: User{UserID: in.ID, Email: "a@b.com"}}, nil
	})
}
