package client

import (
	"fmt"
	"strings"

	"github.com/jkMLnop/binGO-CLI/shared"
)

// DisplayWelcome displays the welcome message and player info to the player
func (p *Player) DisplayWelcome(welcomeMsg ServerMessage) {
	fmt.Printf("\n🎲 Welcome %s!\n", p.PlayerID)
	fmt.Printf("   Game: %s\n", p.GameID)
	fmt.Printf("   Players in game: %v\n", welcomeMsg.Players)

	// Display host info
	if welcomeMsg.HostID != "" {
		hostLabel := "Host"
		if welcomeMsg.HostID == p.PlayerID {
			hostLabel = "Host (you)"
		}
		fmt.Printf("   %s: %s\n", hostLabel, welcomeMsg.HostID)
	}

	// Display game code
	if welcomeMsg.Code != "" {
		fmt.Printf("   Game code: %s\n", welcomeMsg.Code)
	}
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

// DisplaySuggestions prints the pending buzzword suggestions panel (Phase 9).
// Prints nothing when the list is empty.
func (p *Player) DisplaySuggestions(suggestions []Suggestion) {
	if len(suggestions) == 0 {
		return
	}
	border := strings.Repeat("─", 48)
	fmt.Printf("\n┌%s┐\n", border)
	fmt.Printf("│ 📝 Pending Buzzword Suggestions%-16s│\n", "")
	fmt.Printf("├%s┤\n", border)
	for _, s := range suggestions {
		line := fmt.Sprintf("  %-20s  suggested: \"%s\"", s.PlayerID, s.Phrase)
		if len(line) > 48 {
			line = line[:45] + "..."
		}
		fmt.Printf("│ %-48s│\n", line)
	}
	isHost := p.WelcomeMsg.HostID == p.PlayerID
	if isHost {
		fmt.Printf("├%s┤\n", border)
		fmt.Printf("│ approve <phrase>  /  reject <phrase>%-12s│\n", "")
	}
	fmt.Printf("└%s┘\n", border)
}

// DisplayActiveBets prints the active bets panel below the board (Phase 9.5).
// Prints nothing when the list is empty.
func (p *Player) DisplayActiveBets(bets []Bet) {
	if len(bets) == 0 {
		return
	}
	const innerWidth = 54
	border := strings.Repeat("─", innerWidth)
	fmt.Printf("\n┌%s┐\n", border)
	fmt.Printf("│ 🎲 Active Bets%-39s│\n", "") // 🎲 is literal in format string; %-39s adds exact spaces
	fmt.Printf("├%s┤\n", border)
	for _, b := range bets {
		icon := "⏳"
		iconDisplayW := 2 // ⏳ renders as 2 terminal columns (double-width)
		switch b.Status {
		case "won":
			icon = "✓"
			iconDisplayW = 1
		case "lost":
			icon = "✗"
			iconDisplayW = 1
		}
		// Layout: "│ " + icon + " " + body + padding + "│"
		// Inner display cols: 1(space) + iconDisplayW + 1(space) + bodyWidth = innerWidth
		bodyWidth := innerWidth - 2 - iconDisplayW
		body := fmt.Sprintf("%-16s  %s", b.BetterUsername, b.RawText)
		if len(body) > bodyWidth {
			body = body[:bodyWidth-3] + "..."
		}
		// %-*s pads body (ASCII) to bodyWidth runes = bodyWidth display cols
		fmt.Printf("│ %s %-*s│\n", icon, bodyWidth, body)
	}
	fmt.Printf("└%s┘\n", border)
}

// DisplayBetResults prints a bet results summary after a game ends (Phase 9.5).
// Prints nothing when there are no bets.
func (p *Player) DisplayBetResults(bets []Bet) {
	if len(bets) == 0 {
		return
	}
	const innerWidth = 54
	border := strings.Repeat("─", innerWidth)
	fmt.Printf("\n┌%s┐\n", border)
	fmt.Printf("│ 🏅 Bet Results%-39s│\n", "")
	fmt.Printf("├%s┤\n", border)
	for _, b := range bets {
		var icon, label string
		switch b.Status {
		case "won":
			icon = "✓"
			label = "WON "
		case "lost":
			icon = "✗"
			label = "LOST"
		default:
			icon = "?"
			label = "????"
		}
		// "│ ✓ WON  betterUsername  rawText ... │"
		body := fmt.Sprintf("%s  %-16s  %s", label, b.BetterUsername, b.RawText)
		bodyWidth := innerWidth - 4 // "│ " + icon + " " + body + "│"
		if len(body) > bodyWidth {
			body = body[:bodyWidth-3] + "..."
		}
		fmt.Printf("│ %s %-*s│\n", icon, bodyWidth, body)
	}
	fmt.Printf("└%s┘\n", border)
}

// DisplayBuzzwordList prints the full buzzword pool and rejected suggestions (Phase 9.6).
func (p *Player) DisplayBuzzwordList(msg ServerMessage) {
	const innerWidth = 54
	border := strings.Repeat("─", innerWidth)

	fmt.Printf("\n┌%s┐\n", border)
	fmt.Printf("│ 📋 Buzzword Pool (%d words)%-*s│\n", len(msg.FlatBuzzwords), innerWidth-27, "")
	fmt.Printf("├%s┤\n", border)
	if len(msg.FlatBuzzwords) == 0 {
		fmt.Printf("│ %-*s│\n", innerWidth-1, "(none)")
	} else {
		for i, w := range msg.FlatBuzzwords {
			line := fmt.Sprintf("%3d. %s", i+1, w)
			if len(line) > innerWidth-2 {
				line = line[:innerWidth-5] + "..."
			}
			fmt.Printf("│ %-*s│\n", innerWidth-2, line)
		}
	}

	if len(msg.RejectedSuggestions) > 0 {
		fmt.Printf("├%s┤\n", border)
		fmt.Printf("│ ❌ Rejected This Round%-*s│\n", innerWidth-23, "")
		fmt.Printf("├%s┤\n", border)
		for _, r := range msg.RejectedSuggestions {
			line := fmt.Sprintf("  %s", r)
			if len(line) > innerWidth-2 {
				line = line[:innerWidth-5] + "..."
			}
			fmt.Printf("│ %-*s│\n", innerWidth-2, line)
		}
	}
	fmt.Printf("└%s┘\n", border)
}
