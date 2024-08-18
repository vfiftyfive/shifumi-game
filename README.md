# Shifumi Game 🪨📄✂️

Welcome to the Shifumi Game! This is a simple implementation of the classic Rock-Paper-Scissors game using Go and Kafka.

## How to Play 🎮

1. **Launch the Game**: 
   First, ensure you have Docker installed and running. Then, use the following command to start the game using `docker-compose`:

   ```
   docker-compose up -d
   ```

2. **Start a New Game Session**:
   To start a new game session, the first player (Player 1) needs to make their choice. This will create a new session ID.

   ```
   curl -X POST -H "Content-Type: application/json" -d '{"choice":"rock"}' http://localhost:8081/play
   ```

   **Server Response:**

   ```json
   {
     "session_id": "LKiRsa35Ov",
     "player_id": "1",
     "status": "Choice submitted successfully"
   }
   ```

   This response indicates that a new session has been created with `session_id` "LKiRsa35Ov" and Player ID 1 is assigned.

3. **Player 2 Joins the Game**:
   The second player joins the same session by using the session ID provided in the previous step. Player 2 should specify the session ID and their choice.

   ```
   curl -X POST -H "Content-Type: application/json" -d '{"choice":"scissors", "session_id":"LKiRsa35Ov"}' http://localhost:8081/play
   ```

   **Server Response:**

   ```json
   {
     "session_id": "LKiRsa35Ov",
     "player_id": "2",
     "status": "Choice submitted successfully"
   }
   ```

   Player 2 is now registered in the same session.

4. **Continue Playing**:
   Players continue to play rounds until one of them wins three rounds. **Starting from round 2**, you must specify both the session ID and the player ID since they were allocated during round 1.

   **Player 1's turn in Round 2:**

   ```
   curl -X POST -H "Content-Type: application/json" -d '{"player_id":"1", "choice":"paper", "session_id":"LKiRsa35Ov"}' http://localhost:8081/play
   ```

   **Player 2's turn in Round 2:**

   ```
   curl -X POST -H "Content-Type: application/json" -d '{"player_id":"2", "choice":"rock", "session_id":"LKiRsa35Ov"}' http://localhost:8081/play
   ```

5. **Winning the Game**:
   The game ends when one player wins three rounds. The server will notify both players when the game is over.

   **Example of the final round for Player 1:**

   ```
   curl -X POST -H "Content-Type: application/json" -d '{"player_id":"1", "choice":"rock", "session_id":"LKiRsa35Ov"}' http://localhost:8081/play
   ```

   **Server Response:**

   ```json
   {
     "session_id": "LKiRsa35Ov",
     "player_id": "1",
     "status": "Game over. Player 1 wins!"
   }
   ```

   This response indicates that Player 1 has won the game by winning three rounds.

## Project Structure 🏗️

- **api/client/**: Contains the client-side code to interact with the server.
- **api/server/**: Contains the server-side code that handles game logic.
- **cmd/server/**: The entry point for the server application.

## License 📄

This project is licensed under the MIT License.

## Contributing 🤝

Feel free to open issues or submit pull requests if you have any ideas or improvements!

