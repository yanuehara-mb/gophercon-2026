//go:build ignore

package main

import (
	"context"
	"log"
	"os"

	authzed "github.com/authzed/authzed-go/v1"
	v1 "github.com/authzed/authzed-go/proto/authzed/api/v1"
	"github.com/authzed/grpcutil"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func main() {
	addr := "localhost:50051"
	token := "somerandomkeyhere"
	if v := os.Getenv("SPICEDB_ADDR"); v != "" {
		addr = v
	}
	if v := os.Getenv("SPICEDB_TOKEN"); v != "" {
		token = v
	}

	allowedSubject := "client-alice"
	if v := os.Getenv("ALLOWED_SUBJECT"); v != "" {
		allowedSubject = v
	}

	client, err := authzed.NewClient(addr,
		grpcutil.WithInsecureBearerToken(token),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		log.Fatalf("connect: %v", err)
	}

	ctx := context.Background()

	schema := `definition user {}

definition document {
  relation reader: user
  permission read = reader
}`

	_, err = client.WriteSchema(ctx, &v1.WriteSchemaRequest{Schema: schema})
	if err != nil {
		log.Fatalf("write schema: %v", err)
	}
	log.Println("schema written")

	_, err = client.WriteRelationships(ctx, &v1.WriteRelationshipsRequest{
		Updates: []*v1.RelationshipUpdate{
			{
				Operation: v1.RelationshipUpdate_OPERATION_TOUCH,
				Relationship: &v1.Relationship{
					Resource: &v1.ObjectReference{ObjectType: "document", ObjectId: "readme"},
					Relation: "reader",
					Subject:  &v1.SubjectReference{Object: &v1.ObjectReference{ObjectType: "user", ObjectId: allowedSubject}},
				},
			},
		},
	})
	if err != nil {
		log.Fatalf("write relationship: %v", err)
	}
	log.Printf("relationship created: user:%s reader document:readme", allowedSubject)
	log.Println("SpiceDB seeded successfully.")
}
