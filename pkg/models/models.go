package models

const MaxPlayers = 2

type PlayerChoice struct {
	PlayerID    string `json:"player_id"`
	SessionID   string `json:"session_id"`
	Choice      string `json:"choice"`
	InitSession bool   `json:"init_session"`
}

type RoundResult struct {
	RoundNumber int           `json:"round_number"`
	Player1     *PlayerChoice `json:"player1"`
	Player2     *PlayerChoice `json:"player2"`
	Result      string        `json:"result"` // Outcome message after each round
}

type GameSession struct {
	SessionID        string        `json:"session_id"`
	Status           string        `json:"status"`
	CurrentRound     int           `json:"round"`
	Player1HasPlayed bool          `json:"player_1_has_played"`
	Player2HasPlayed bool          `json:"player_2_has_played"`
	Results          []RoundResult `json:"results"`
	Player1Wins      int           `json:"player1_wins"`
	Player2Wins      int           `json:"player2_wins"`
	Draws            int           `json:"draws"`
	Winner           string        `json:"winner"`
}

// Setter for the winner
func (gs *GameSession) SetWinner(winner string) {
	gs.Winner = winner
}

// Getter for the winner
func (gs *GameSession) GetWinner() string {
	return gs.Winner
}

// NewGameSession creates a new GameSession with default values
func NewGameSession(sessionID string) *GameSession {
	return &GameSession{
		SessionID:        sessionID,
		Status:           "in progress",
		Results:           []RoundResult{{RoundNumber: 1}},
		CurrentRound:     1,
		Player1HasPlayed: false,
		Player2HasPlayed: false,
	}
}

// HasPlayer1Played returns whether Player 1 has played this round
func (s *GameSession) HasPlayer1Played() bool {
	return s.Player1HasPlayed
}

// SetPlayer1HasPlayed sets whether Player 1 has played this round
func (s *GameSession) SetPlayer1HasPlayed(hasPlayed bool) {
	s.Player1HasPlayed = hasPlayed
}

// HasPlayer2Played returns whether Player 2 has played this round
func (s *GameSession) HasPlayer2Played() bool {
	return s.Player2HasPlayed
}

// SetPlayer2HasPlayed sets whether Player 2 has played this round
func (s *GameSession) SetPlayer2HasPlayed(hasPlayed bool) {
	s.Player2HasPlayed = hasPlayed
}
