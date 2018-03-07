package main

import (
	firebase "firebase.google.com/go"
	"golang.org/x/net/context"
	"google.golang.org/api/iterator"
	"log"
)

func main() {

	// Pulls credentials from env var
	app, err := firebase.NewApp(context.Background(), nil)
	if err != nil {
		log.Fatalf("firebase app creation error")
	}
	client, err := app.Auth(context.Background())
	if err != nil {
		log.Fatalf("error getting Auth client")
	}

	iter := client.Users(context.Background(), "")
	for {
		user, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			log.Fatalf("error listing users: %s\n", err)
		}
		log.Printf("read user user: %v\n", user)
		err = client.DeleteUser(context.Background(), user.UserRecord.UserInfo.UID)
		if err != nil {
			log.Fatalf("error deleting user: %v\n", err)
		}
		log.Printf("Successfully deleted user: %s\n", user.UserRecord.UserInfo.UID)
	}

}
