package models

type PlayerChoice struct {
	PlayerID  int    `json:"player_id"`
	Choice    string `json:"choice"`
	SessionID string `json:"session_id"`
}

type GameResult struct {
	SessionID   string `json:"session_id"`
	Player1Wins int    `json:"player1_wins"`
	Player2Wins int    `json:"player2_wins"`
	Draws       int    `json:"draws"`
	Status      string `json:"status"`
	Message     string `json:"message"`
	Player1     string `json:"player1_choice,omitempty"` // Adding player choices to the game result
	Player2     string `json:"player2_choice,omitempty"`
}

type GameSession struct {
	SessionID   string
	Player1     *PlayerChoice
	Player2     *PlayerChoice
	Player1Wins int
	Player2Wins int
	Draws       int
	Status      string
}
