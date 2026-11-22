package engine_test

import "github.com/networkteam/sdd/internal/engine"

type memorySink struct{ events []engine.Event }

func (m *memorySink) Append(e engine.Event) (uint64, error) {
	m.events = append(m.events, e)
	return uint64(e.Seq), nil
}

const briefLoopMachine = `state:
    brief: {type: text, desc: the answer the command judges}
steps:
    - id: ask
      chooser: agent
      options:
          - {choice: send, collect: [brief], call: checkBrief, to: confirm}
    - id: confirm
      chooser: agent
      options:
          - {choice: again, to: ask}
          - {choice: finish, to: end(completed)}`
