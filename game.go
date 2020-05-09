package appleappleorange

import (
	"errors"
	"math/rand"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

type Game struct {
	ID        string     `json:"id"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	Words     WordPair   `json:"word_pair"`
	Players   []*User    `json:"players"`
	Observers []*User    `json:"observers"`
	AltPlayer uuid.UUID  `json:"alt_player"`
	Win       *Win       `json:"win,omitempty"`
	Rounds    []*Round   `json:"rounds"` // chronological
	Log       []LogEvent `json:"log"`    // chronological
}

type WordPair struct {
	Primary string `json:"primary"`
	Alt     string `json:"alt"`
}

type EventType string

const (
	EventClue                       = "clue"
	EventNextRound                  = "next_round"
	EventSuddenDeathRound           = "sudden_death_round"
	EventAppleVotedOut              = "event_apple_voted_out"
	EventOrangeVotedOut             = "event_orange_voted_out"
	EventOrangeSurvived             = "orange_survived"
	EventAppleThoughtItWasTheOrange = "apple_thought_it_was_the_orange"
	EventOrangeGuessedRight         = "orange_guessed_right"
	EventOrangeGuessedWrong         = "orange_guessed_wrong"
)

type LogEvent struct {
	Type    EventType   `json:"type"`
	UserID  uuid.UUID   `json:"user_id"`
	UserIDs []uuid.UUID `json:"user_ids,omitempty"`
	Guess   string      `json:"guess,omitempty"` // for IAmTheOrange events
	Clue    string      `json:"clue,omitempty"`  // for Clue events
}

type User struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}

type Round struct {
	IsSuddenDeath      bool                    `json:"is_sudden_death"`
	Clues              map[uuid.UUID]string    `json:"clues"`
	Votes              map[uuid.UUID]uuid.UUID `json:"votes"`
	PlayersStillIn     map[uuid.UUID]bool      `json:"players_still_in"`
	PlayersGivingClues map[uuid.UUID]bool      `json:"players_giving_clues"`
	UsersVoting        map[uuid.UUID]bool      `json:"users_voting"`
	PlayerEliminated   *uuid.UUID              `json:"player_eliminated,omitempty"`
	RoundStartedAt     time.Time               `json:"round_started_at"`
	VotingStartedAt    *time.Time              `json:"voting_started_at,omitempty"`
}

type Win struct {
	Winners []uuid.UUID `json:"winners"`
	Why     string      `json:"why"`
}

func NewGame(words WordPair, players, observers []*User) *Game {
	playersStillIn := make(map[uuid.UUID]bool, len(players))
	playersGivingClues := make(map[uuid.UUID]bool, len(players))
	usersVoting := make(map[uuid.UUID]bool, len(players)+len(observers))
	for _, p := range players {
		playersStillIn[p.ID] = true
		playersGivingClues[p.ID] = true
		usersVoting[p.ID] = true
	}
	for _, o := range observers {
		usersVoting[o.ID] = true
	}

	return &Game{
		CreatedAt: time.Now(),
		Words:     words,
		Players:   players,
		Observers: observers,
		AltPlayer: players[rand.Intn(len(players))].ID,
		Win:       nil,
		Rounds: []*Round{
			{
				IsSuddenDeath:      false,
				Clues:              make(map[uuid.UUID]string),
				Votes:              make(map[uuid.UUID]uuid.UUID),
				PlayersStillIn:     playersStillIn,
				PlayersGivingClues: playersGivingClues,
				UsersVoting:        usersVoting,
			},
		},
	}
}

func (g *Game) CurrentRound() *Round {
	return g.Rounds[len(g.Rounds)-1]
}

func (g *Game) GiveClue(userID uuid.UUID, clue string) error {
	rnd := g.CurrentRound()
	if !rnd.PlayersGivingClues[userID] {
		return errors.New("You don't need to give clues right now.")
	}
	if c := rnd.Clues[userID]; len(c) > 0 {
		return errors.New("You already gave a clue and can't change it.")
	}

	rnd.Clues[userID] = clue
	g.UpdatedAt = time.Now()
	g.Log = append(g.Log, LogEvent{
		Type:   EventClue,
		UserID: userID,
		Clue:   clue,
	})

	// If everyone has given their clues, trigger voting.
	if len(rnd.Clues) == len(rnd.PlayersGivingClues) {
		now := time.Now()
		rnd.VotingStartedAt = &now
	}
	return nil
}

func (g *Game) Vote(userID uuid.UUID, vote uuid.UUID) error {
	rnd := g.CurrentRound()
	if !rnd.UsersVoting[userID] {
		return errors.New("You're not allowed to vote now.")
	}
	if len(rnd.Clues) < len(rnd.PlayersGivingClues) {
		return errors.New("Everyone needs to submit their clues first.")
	}
	if !rnd.PlayersGivingClues[vote] {
		return errors.New("That player isn't up for elimination.")
	}

	rnd.Votes[userID] = vote
	g.UpdatedAt = time.Now()
	// If everyone has voted, process the vote count.
	if len(rnd.Votes) == len(rnd.UsersVoting) {
		g.tallyVotes(rnd)
	}
	return nil
}

func (g *Game) IAmTheOrange(userID uuid.UUID, guess string) error {
	rnd := g.CurrentRound()
	if g.Win != nil {
		return errors.New("The game is already over.")
	}
	if !rnd.PlayersStillIn[userID] {
		return errors.New("You're not an active player in this game.")
	}

	if g.AltPlayer == userID {
		g.UpdatedAt = time.Now()

		// TODO: This isn't great. What if they spell it slightly off or guess a
		// word that is 'close enough?'. Need to revisit these mechanics.
		if strings.ToUpper(strings.TrimSpace(guess)) == strings.ToUpper(strings.TrimSpace(g.Words.Primary)) {
			g.Win = &Win{Winners: []uuid.UUID{userID}, Why: "orange_guessed_apple"}
			g.Log = append(g.Log, LogEvent{
				Type:   EventOrangeGuessedRight,
				UserID: userID,
			})
			return nil
		}

		var winners []uuid.UUID
		for w := range rnd.PlayersStillIn {
			if w != userID {
				winners = append(winners, w)
			}
		}
		g.Win = &Win{Winners: winners, Why: "orange_guessed_wrong"}
		g.Log = append(g.Log, LogEvent{
			Type:   EventOrangeGuessedWrong,
			UserID: userID,
		})
		return nil
	}

	g.UpdatedAt = time.Now()
	delete(rnd.PlayersStillIn, userID)
	delete(rnd.PlayersGivingClues, userID)
	delete(rnd.UsersVoting, userID)
	g.Log = append(g.Log, LogEvent{
		Type:   EventAppleThoughtItWasTheOrange,
		UserID: userID,
		Guess:  guess,
	})
	return nil
}

func (g *Game) tallyVotes(rnd *Round) {
	votes := map[uuid.UUID]int{}
	var ballotsCast int
	for _, v := range rnd.Votes {
		votes[v] = votes[v] + 1
		ballotsCast++
	}
	var tallies []tally
	for userID, count := range votes {
		tallies = append(tallies, tally{userID: userID, votes: count})
	}
	sort.Sort(byVotes(tallies))

	// Check if the user w/ the most votes has a majority.
	if tallies[0].votes*2 > ballotsCast {
		rnd.PlayerEliminated = &tallies[0].userID
		g.Log = append(g.Log, LogEvent{
			Type:   EventAppleVotedOut,
			UserID: tallies[0].userID,
		})

		// Was the eliminated player the orange?
		if *rnd.PlayerEliminated == g.AltPlayer {
			// You're a winner baby.
			stillIn := userIDs(rnd.PlayersStillIn)
			winners := removeUser(stillIn, g.AltPlayer)
			g.Win = &Win{
				Winners: winners,
				Why:     "orange_voted_out",
			}
			g.Log = append(g.Log, LogEvent{
				Type:   EventOrangeVotedOut,
				UserID: g.AltPlayer,
			})
			return
		}

		// Did the orange just survive to the final two?
		if len(rnd.PlayersStillIn) == 3 {
			g.Win = &Win{
				Winners: []uuid.UUID{g.AltPlayer},
				Why:     "orange_surived",
			}
			g.Log = append(g.Log, LogEvent{
				Type:   EventOrangeSurvived,
				UserID: g.AltPlayer,
			})
			return
		}

		// The eliminated player was NOT the orange.
		// Move on to the next normal round, but without the eliminated player.
		playersStillIn := copyUserIDs(rnd.PlayersStillIn)
		delete(playersStillIn, *rnd.PlayerEliminated)

		usersVoting := map[uuid.UUID]bool{}
		for userID := range rnd.UsersVoting {
			if userID != *rnd.PlayerEliminated {
				usersVoting[userID] = true
			}
		}
		g.Log = append(g.Log, LogEvent{
			Type:    EventNextRound,
			UserIDs: userIDs(playersStillIn),
		})
		g.Rounds = append(g.Rounds, &Round{
			IsSuddenDeath:      false,
			Clues:              make(map[uuid.UUID]string, len(playersStillIn)),
			Votes:              make(map[uuid.UUID]uuid.UUID, len(usersVoting)),
			PlayersStillIn:     playersStillIn,
			PlayersGivingClues: playersStillIn,
			UsersVoting:        usersVoting,
			PlayerEliminated:   nil,
			RoundStartedAt:     time.Now(),
			VotingStartedAt:    nil,
		})
		return
	}

	// There isn't a majority, so there needs to be a sudden death round.
	suddenDeathPlayers := map[uuid.UUID]bool{}
	suddenDeathPlayers[tallies[0].userID] = true
	suddenDeathPlayers[tallies[1].userID] = true
	// We might need to include additional players if there was a tie for
	// plurality.
	for i := 2; i < len(tallies) && tallies[i].votes == tallies[0].votes; i++ {
		suddenDeathPlayers[tallies[i].userID] = true
	}

	g.Log = append(g.Log, LogEvent{
		Type:    EventSuddenDeathRound,
		UserIDs: userIDs(suddenDeathPlayers),
	})

	// NB: We allow sudden death players to vote even when they're the ones up
	// for elimination, because it handles the scenario where all remaining
	// players tie and move on to a sudden death round.

	g.Rounds = append(g.Rounds, &Round{
		IsSuddenDeath:      true,
		Clues:              make(map[uuid.UUID]string, len(suddenDeathPlayers)),
		Votes:              make(map[uuid.UUID]uuid.UUID, len(rnd.UsersVoting)),
		PlayersStillIn:     rnd.PlayersStillIn, // no one eliminated
		PlayersGivingClues: suddenDeathPlayers,
		UsersVoting:        rnd.UsersVoting, // no one eliminated
		PlayerEliminated:   nil,
		RoundStartedAt:     time.Now(),
		VotingStartedAt:    nil,
	})
}

func copyUserIDs(m map[uuid.UUID]bool) map[uuid.UUID]bool {
	m2 := make(map[uuid.UUID]bool, len(m))
	for id, v := range m {
		m2[id] = v
	}
	return m2
}

func removeUser(s []uuid.UUID, remove uuid.UUID) []uuid.UUID {
	for i, userID := range s {
		if userID == remove {
			s = append(s[:i], s[i+1:]...)
			return s
		}
	}
	return s
}

func userIDs(m map[uuid.UUID]bool) []uuid.UUID {
	ids := make([]uuid.UUID, 0, len(m))
	for uID := range m {
		ids = append(ids, uID)
	}
	return ids
}

type tally struct {
	userID uuid.UUID
	votes  int
}

type byVotes []tally

func (s byVotes) Len() int           { return len(s) }
func (s byVotes) Less(i, j int) bool { return s[i].votes < s[j].votes }
func (s byVotes) Swap(i, j int)      { s[i], s[j] = s[j], s[i] }
