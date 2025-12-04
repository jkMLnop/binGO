package client

import (
	"fmt"

	"github.com/jkMLnop/binGO-CLI/shared"
)

// DisplayWelcome displays the welcome message and player info to the player
func (p *Player) DisplayWelcome(welcomeMsg ServerMessage) {
	fmt.Printf("\n🎲 Welcome %s!\n", p.PlayerID)
	fmt.Printf("   Game: %s\n", p.GameID)
	fmt.Printf("   Players in game: %v\n", welcomeMsg.Players)
}

// DisplayGameEnd displays the game end message and handles win animation
func (p *Player) DisplayGameEnd(msg ServerMessage) {
	fmt.Printf("\n\n🏆 Game Ended! Winner: %s\n", msg.Winner)
	fmt.Printf("   %s\n\n", msg.Message)
	if msg.Winner == p.PlayerID {
		fmt.Println("🎊 You won!")
		// Show win animation for the winner
		shared.DisplayWinScreen()
	}
}
