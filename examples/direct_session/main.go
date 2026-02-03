package main

import (
	"fmt"
	"log"

	"github.com/Desarso/godantic"
	"github.com/Desarso/godantic/common_tools"
	"github.com/Desarso/godantic/models/gemini"
	"github.com/Desarso/godantic/stores"
)

func main() {

	fmt.Println("=== Direct Session Usage Example ===")

	// Create agent
	tools, err := godantic.Create_Tools([]interface{}{
		common_tools.Search,
	})
	if err != nil {
		log.Fatal(err)
	}

	agent := godantic.Create_Agent(&gemini.Gemini_Model{
		Model: "gemini-2.0-flash",
	}, tools, nil)

	// Create store
	store, err := stores.NewSQLiteStoreSimple("direct_session.sqlite")
	if err != nil {
		log.Fatal(err)
	}

	// Create session
	session := godantic.NewHTTPSession("direct_example", &agent, store)

	// Single interaction
	// userMsg := models.User_Message{
	// 	Content: models.Content{
	// 		Parts: []models.User_Part{
	// 			{Text: "Who's the president?"},
	// 		},
	// 	},
	// }

	// response, err := session.RunSingleInteraction(userMsg)
	// if err != nil {
	// 	log.Printf("Error: %v", err)
	// 	return
	// }

	// // Process and display the response properly
	// fmt.Println("\n=== Response ===")
	// for i, part := range response.Parts {
	// 	fmt.Printf("Part %d:\n", i+1)

	// 	// Display text content
	// 	if part.Text != nil {
	// 		fmt.Printf("  Text: %s\n", *part.Text)
	// 	}

	// 	// // Display function call content
	// 	// if part.FunctionCall != nil {
	// 	// 	fmt.Printf("  Function Call:\n")
	// 	// 	fmt.Printf("    Name: %s\n", part.FunctionCall.Name)
	// 	// 	fmt.Printf("    Args: %+v\n", part.FunctionCall.Args)
	// 	// 	if part.FunctionCall.ID != "" {
	// 	// 		fmt.Printf("    ID: %s\n", part.FunctionCall.ID)
	// 	// 	}
	// 	// }

	// 	// // If neither text nor function call, indicate empty part
	// 	// if part.Text == nil && part.FunctionCall == nil {
	// 	// 	fmt.Printf("  (Empty part)\n")
	// 	// }
	// 	fmt.Println()
	// }

	// Get chat history
	history, err := session.GetChatHistory()
	if err != nil {
		log.Printf("Error getting history: %v", err)
		return
	}

	fmt.Printf("\n=== Chat History (%d messages) ===\n", len(history))
	for i, msg := range history {
		fmt.Printf("Message %d:\n", i+1)
		fmt.Printf("  Role: %s\n", msg.Role)
		fmt.Printf("  Type: %s\n", msg.Type)
		if msg.Text != "" {
			fmt.Printf("  Text: %s\n", msg.Text)
		}
		if msg.FunctionID != "" {
			fmt.Printf("  Function ID: %s\n", msg.FunctionID)
		}
		fmt.Printf("  Created: %s\n", msg.CreatedAt.Format("2006-01-02 15:04:05"))
		fmt.Println()
	}
}
