// Package types implements different core types of the API.
package types

import "go.mongodb.org/mongo-driver/bson"

// TokenTransactionList represents a list of ERC20/ERC721/ERC1155 transactions.
type TokenTransactionList struct {
	// List keeps the actual Collection.
	Collection []*TokenTransaction

	// Total indicates total number of ERC transactions in the whole collection.
	Total uint64

	// First is the index of the first item on the list
	First uint64

	// Last is the index of the last item on the list
	Last uint64

	// IsStart indicates there are no ERC transactions available above the list currently.
	IsStart bool

	// IsEnd indicates there are no ERC transactions available below the list currently.
	IsEnd bool

	// Filter represents the base filter used for filtering the list
	Filter bson.D
	cursor *string
}

// Reverse reverses the order of ERC transactions in the list.
func (c *TokenTransactionList) Reverse() {
	// anything to swap at all?
	if c.Collection == nil || len(c.Collection) < 2 {
		return
	}

	// swap elements
	for i, j := 0, len(c.Collection)-1; i < j; i, j = i+1, j-1 {
		c.Collection[i], c.Collection[j] = c.Collection[j], c.Collection[i]
	}

	// swap indexes
	c.First, c.Last = c.Last, c.First
}

// // Package types implements different core types of the API.
// package types

// import (
// 	"fmt"
// )

// // TokenTransactionList represents a list of ERC20/ERC721/ERC1155 transactions.
// type TokenTransactionList struct {
// 	// List keeps the actual collection of token transactions.
// 	Collection []*TokenTransaction

// 	// Total indicates total number of ERC transactions in the whole collection.
// 	Total uint64

// 	// First is the index of the first item in the list.
// 	First uint64

// 	// Last is the index of the last item in the list.
// 	Last uint64

// 	// IsStart indicates there are no ERC transactions available above the list currently.
// 	IsStart bool

// 	// IsEnd indicates there are no ERC transactions available below the list currently.
// 	IsEnd bool

// 	// Filter represents the base filter used for filtering the list (PostgreSQL-friendly).
// 	Filter SQLFilter

// 	// Cursor represents pagination or query continuation.
// 	cursor *string
// }

// // SQLFilter represents the structure for SQL queries filtering.
// type SQLFilter struct {
// 	TokenType       string
// 	Token           string
// 	TokenID         string
// 	Sender          string
// 	Recipient       string
// 	TransactionType []int32
// }

// // Reverse reverses the order of ERC transactions in the list.
// // // Reverse reverses the order of ERC transactions in the list.
// func (c *TokenTransactionList) Reverse() {
// 	// anything to swap at all?
// 	if c.Collection == nil || len(c.Collection) < 2 {
// 		return
// 	}

// 	// swap elements
// 	for i, j := 0, len(c.Collection)-1; i < j; i, j = i+1, j-1 {
// 		c.Collection[i], c.Collection[j] = c.Collection[j], c.Collection[i]
// 	}

// 	// swap indexes
// 	c.First, c.Last = c.Last, c.First
// }

// // BuildQuery builds the SQL WHERE clause from the provided SQLFilter for PostgreSQL queries.
// func (f *SQLFilter) BuildQuery() (string, []interface{}, error) {
// 	var conditions []string
// 	var args []interface{}

// 	if f.TokenType != "" {
// 		conditions = append(conditions, "token_type = ?")
// 		args = append(args, f.TokenType)
// 	}

// 	if f.Token != "" {
// 		conditions = append(conditions, "token = ?")
// 		args = append(args, f.Token)
// 	}

// 	if f.TokenID != "" {
// 		conditions = append(conditions, "token_id = ?")
// 		args = append(args, f.TokenID)
// 	}

// 	if f.Sender != "" {
// 		conditions = append(conditions, "(sender = ? OR recipient = ?)")
// 		args = append(args, f.Sender, f.Sender)
// 	}

// 	if len(f.TransactionType) > 0 {
// 		conditions = append(conditions, "transaction_type = ANY(?)")
// 		args = append(args, f.TransactionType)
// 	}

// 	// If no conditions, just return an empty query
// 	if len(conditions) == 0 {
// 		return "", nil, fmt.Errorf("no filter conditions provided")
// 	}

// 	query := "WHERE " + stringJoin(conditions, " AND ")
// 	return query, args, nil
// }

// // stringJoin is a helper function to join conditions.
// func stringJoin(strs []string, sep string) string {
// 	result := ""
// 	for i, str := range strs {
// 		if i > 0 {
// 			result += sep
// 		}
// 		result += str
// 	}
// 	return result
// }
