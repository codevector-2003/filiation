# Retrieval test set

Twenty questions, each answered by one paper, for grading how well fil finds
the paper that answers a question. It is the test set M4 is measured against,
written before the retriever it grades.

`questions.json` names each question's paper by OpenAlex ID, so the set works on
any library that holds those papers, and gives the answer and page as the paper
states them. The questions are worded as a researcher would ask, not in the
passage's own words.

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

| Method | Paper in first three | Mean reciprocal rank |
| --- | --- | --- |
| keyword (`fil search`, M3) | 13 of 20 | 0.60 |

Keyword search misses q04, q05, q06, q14, q15, q16 and q17. A smaller library
with 65 searchable papers gave 14 of 20: more papers mean more near misses.

The target for M4 is 15 of 20 (`docs/PRODUCT_PLAN.md`), and hybrid retrieval
must beat plain vector search.

Keyword search misses where the question uses words the paper does not:
"compulsory" for a mandate, "free-to-read" for open access, "pay their journal"
for author-choice open access. That is the gap meaning search should close.

The score flatters keyword search. These questions were written after reading
the passages, and some of their words still match.
