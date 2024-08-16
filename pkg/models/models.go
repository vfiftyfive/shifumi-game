package models

type PlayerChoice struct {
	PlayerID  string `json:"player_id"`
	SessionID string `json:"session_id"`
	Choice    string `json:"choice"`
}

type GameSession struct {
	SessionID        string        `json:"session_id"`
	Status           string        `json:"status"`
	Player1          *PlayerChoice `json:"player1"`
	Player2          *PlayerChoice `json:"player2"`
	Player1HasPlayed bool          `json:"player1_has_played"` // Whether Player 1 has played this round
	Player2HasPlayed bool          `json:"player2_has_played"` // Whether Player 2 has played this round
	Round            int           `json:"round"`
	Player1Wins      int           `json:"player1_wins"`
	Player2Wins      int           `json:"player2_wins"`
	Draws            int           `json:"draws"`
	ResultMessage    string        `json:"message"` // Outcome message after each round
	Player1Choice    string        `json:"player1_choice,omitempty"`
	Player2Choice    string        `json:"player2_choice,omitempty"`
}
