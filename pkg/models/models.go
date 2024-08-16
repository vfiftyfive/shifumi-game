package models

type PlayerChoice struct {
	PlayerID  string `json:"player_id"`
	SessionID string `json:"session_id"`
	Choice    string `json:"choice"`
}

type GameSession struct {
	SessionID     string        `json:"session_id"`
	Status        string        `json:"status"`
	Round         int           `json:"round"`
	Player1Wins   int           `json:"player1_wins"`
	Player2Wins   int           `json:"player2_wins"`
	Draws         int           `json:"draws"`
	ResultMessage string        `json:"message"` // Outcome message after each round
	Rounds        []RoundResult `json:"rounds"`  // New: Slice to store the results of each round
}

type RoundResult struct {
	RoundNumber int           `json:"round_number"`
	Player1     *PlayerChoice `json:"player1"`
	Player2     *PlayerChoice `json:"player2"`
	Result      string        `json:"result"` // "Player 1 wins", "Player 2 wins", "Draw"
}
