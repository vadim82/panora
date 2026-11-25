package playback

import (
	"slices"

	"github.com/vadim82/panora/internal/catalog"
)

// freshDraws keeps alternatives and current-image repeats in separate draws.
// The two balance draws share the provider round, as in ordinary playback.
func freshDraws(mode string, alternatives, repeats []providerPool, draw func(int) int) (next, repeat func() (catalog.Candidate, bool)) {
	if mode == rotationShuffleAllImages {
		return shuffleDraw(alternatives, draw), shuffleDraw(repeats, draw)
	}
	blocked := map[string]bool{}
	next = func() (catalog.Candidate, bool) {
		return drawCandidate(alternatives, blocked, draw)
	}
	repeat = func() (catalog.Candidate, bool) {
		return drawCandidate(repeats, blocked, draw)
	}
	return next, repeat
}

type shuffleIdentity struct {
	imageID int64
	hash    string
}

func shuffleDraw(pools []providerPool, draw func(int) int) func() (catalog.Candidate, bool) {
	// Catalog IDs are shared across search memberships. A known content hash
	// also joins aliases with different IDs, including copies across providers.
	indices := make(map[shuffleIdentity]int)
	var available [][]catalog.Candidate
	for _, provider := range pools {
		for _, input := range provider.inputs {
			for _, candidate := range input {
				key := shuffleIdentity{hash: candidate.Hash}
				if key.hash == "" {
					key.imageID = candidate.Identity.ImageID
				}
				index, exists := indices[key]
				if !exists {
					index = len(available)
					indices[key] = index
					available = append(available, nil)
				}
				available[index] = append(available[index], candidate)
			}
		}
	}
	var retry [][]catalog.Candidate
	var eligible []int
	blocked := map[string]bool{}
	untriedProvider := func(candidate catalog.Candidate) bool {
		return !blocked[candidate.Identity.ProviderID]
	}
	return func() (catalog.Candidate, bool) {
		if len(available) == 0 {
			available, retry = retry, available
		}
		if len(available) == 0 {
			return catalog.Candidate{}, false
		}
		// The first draw includes every distinct image. After a failed
		// attempt, give other providers a turn before retrying that provider.
		eligible = eligible[:0]
		if len(blocked) != 0 {
			for i, memberships := range available {
				if slices.ContainsFunc(memberships, untriedProvider) {
					eligible = append(eligible, i)
				}
			}
		}
		var index int
		if len(eligible) == 0 {
			clear(blocked)
			index = draw(len(available))
		} else {
			index = eligible[draw(len(eligible))]
		}
		memberships := available[index]
		last := len(available) - 1
		available[index] = available[last]
		available[last] = nil
		available = available[:last]

		// Give other images a turn before trying another membership of a
		// failed image. Each membership retains its own source rules.
		eligible = eligible[:0]
		for i, candidate := range memberships {
			if untriedProvider(candidate) {
				eligible = append(eligible, i)
			}
		}
		member := eligible[0]
		if len(eligible) > 1 {
			member = eligible[draw(len(eligible))]
		}
		picked := memberships[member]
		blocked[picked.Identity.ProviderID] = true
		last = len(memberships) - 1
		memberships[member] = memberships[last]
		if last > 0 {
			retry = append(retry, memberships[:last])
		}
		return picked, true
	}
}
