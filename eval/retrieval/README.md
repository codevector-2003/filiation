# Retrieval test set

Twenty questions, each answered by one paper, for grading how well fil finds
the paper that answers a question. It is the test set M4 is measured against,
written before the retriever it grades.

`questions.json` names each question's paper by OpenAlex ID, so the set works on
any library that holds those papers, and gives the answer and page as the paper
states them. The questions are worded as a researcher would ask, not in the
passage's own words.

## Two sets

- `questions.json` is the **working set**. Look at its misses and tune against it.
- `heldout.json` is the **locked set**: 20 more questions, each on a different
  paper from the working set, written before any tuning began. Score it only to
  report a change, and never look at which of its questions fail, or it stops
  measuring anything. A change counts only if it improves both sets.

## Score a method

```
go run ./eval/retrieval -library <folder> [-method keyword] [-v]
```

A method returns passages, best first. The papers they come from, in order of
first appearance, are its ranking. A question counts as a hit when its paper is
in the first three. The score is the number of hits and the mean reciprocal
rank. `-v` lists the first five papers each question found.

## The library it was written against

Every paper is in the library grown from the PeerJ seed:

```
fil library new <folder>
fil --library <folder> add 10.7717/peerj.4375
fil --library <folder> expand --max-nodes 400
fil --library <folder> fetch --max 200
fil --library <folder> extract
```

Built on 15 Nov 2026 this gave 11,787 works, 469 of them fetched, and 116
papers with searchable text. It cost 25 OpenAlex credits. OpenAlex changes over
time, so a later run may fetch a slightly different library. A question whose
paper is missing is reported, not counted.

## Results

| Search | Working set: first three | MRR | Locked set: first three | MRR |
| --- | --- | --- | --- | --- |
| M3: every word, else any; each paper placed by its best passage | 13 of 20 | 0.60 | 15 of 20 | 0.60 |
| M4 (D35): filler words left out; papers ranked by their best three passages, fused with title and abstract | **15 of 20** | **0.73** | **15 of 20** | **0.68** |

`-method keyword` always runs what `fil search` does now, so these were measured
by running it before and after the change.

The prototypes behind D35 were measured here first, and D35 records them. Two
ideas did not help: citing sentences as evidence about the cited paper, and title
and abstract scored only among the matched papers rather than across the library.

The plan's target is 15 of 20 (`docs/PRODUCT_PLAN.md`). The gain on the locked
set is in rank: the paper that answers moves up, but the count in the first three
stays 15. A change to the ranking must beat these figures on both sets.

Search still misses where the question uses words the paper does not, such as
"compulsory" for a mandate or "free-to-read" for open access.

For reference, spike 14 scored plain meaning search over every passage on the
working set: Ollama's nomic-embed-text 15 of 20 (MRR 0.76), and all-MiniLM-L6-v2
run in pure Go 10 of 20 (MRR 0.41). Neither is built (D35).

The working set flatters any method tuned on it. Its questions were written after
reading the passages, and the ranking was tuned against it; the locked set is the
fair figure.
