package models

const MaxPlayers = 2

type PlayerChoice struct {
	PlayerID  string `json:"player_id"`
	SessionID string `json:"session_id"`
	Choice    string `json:"choice"`
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
	player1HasPlayed bool          `json:"-"`
	player2HasPlayed bool          `json:"-"`
	Rounds           []RoundResult `json:"rounds"`
	Player1Wins      int           `json:"player1_wins"`
	Player2Wins      int           `json:"player2_wins"`
	Draws            int           `json:"draws"`
	winner           string
}

// Setter for the winner
func (gs *GameSession) SetWinner(winner string) {
	gs.winner = winner
}

// Getter for the winner
func (gs *GameSession) GetWinner() string {
	return gs.winner
}

// NewGameSession creates a new GameSession with default values
func NewGameSession(sessionID string) *GameSession {
	return &GameSession{
		SessionID:        sessionID,
		Status:           "in progress",
		Rounds:           []RoundResult{{RoundNumber: 1}},
		CurrentRound:     1,
		player1HasPlayed: false,
		player2HasPlayed: false,
	}
}

// HasPlayer1Played returns whether Player 1 has played this round
func (s *GameSession) HasPlayer1Played() bool {
	return s.player1HasPlayed
}

// SetPlayer1HasPlayed sets whether Player 1 has played this round
func (s *GameSession) SetPlayer1HasPlayed(hasPlayed bool) {
	s.player1HasPlayed = hasPlayed
}

// HasPlayer2Played returns whether Player 2 has played this round
func (s *GameSession) HasPlayer2Played() bool {
	return s.player2HasPlayed
}

// SetPlayer2HasPlayed sets whether Player 2 has played this round
func (s *GameSession) SetPlayer2HasPlayed(hasPlayed bool) {
	s.player2HasPlayed = hasPlayed
}
