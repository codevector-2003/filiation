# Product development plan

Draft 2 · 29 August 2026 · covers Sept 2026 → late May 2027

> **Revised for Go.** Phase 1 gains two weeks for learning the language while building; Phase 3
> gains one because `quelle` can no longer be reused; Phase 5 loses one because `//go:embed`
> ships the web UI inside the binary. Net: about three weeks later than the Python plan.

## Assumptions this plan is built on

State these here so they can be corrected rather than silently wrong.

- One person, roughly **10 hours a week**, alongside coursework at Moratuwa.
- Exam periods in **December–January** and **April–May** will cut that in half. The dates below
  already assume it.
- This is a side project that **could become the final-year project** later. It is not currently
  bound to any university deadline.
- No budget. Everything must run free, for the maintainer and the user.
- **Written in Go, learned while building.** The maintainer's prior projects are all Python. The
  dates below carry that cost rather than hiding it.

---

## 1. Positioning

**For** researchers and graduate students who read more papers than they can hold in their head,
**Filiation** is a citation graph and retrieval engine that runs on their own machine.
**Unlike** Connected Papers, ResearchRabbit and Litmaps, which give you a picture for one query
and forget it when you close the tab, Filiation builds a library that grows over years and that
an AI assistant can query directly.

### The wedge

Three things make this different. Everything else is table stakes.

1. **It accumulates.** Your map of everything you have read, not a per-query snapshot.
2. **It is queryable by an agent.** An MCP server, not a picture. Ask "what connects these two
   papers", "what is highly co-cited that I do not have", "which of my papers disagree".
3. **It knows why one paper cites another.** Method, dataset, comparison, or disagreement — and
   from that, it can trace a claim back to the paper that first made it.

### Who it is not for

People who want a pretty map of one query. That market is served, well, by free tools. Do not
compete there.

---

## 2. The biggest risk is not technical

You can build all of this. The real risk is spending nine months on something nobody wants.
So the plan starts with two weeks of talking to people, before any real code.

Most student projects skip this and then wonder why the repo has three stars and no users.

---

## 3. Phases

### Phase 0 — Validate · 1–14 Sept 2026

**Do:** Talk to 8–10 people who read papers seriously. Postgrads in your own department are the
easiest start; add a few from other fields, because a computer scientist's habits are not a
biologist's.

Ask about their actual behaviour, not your idea:

- Walk me through the last time you followed a reference chain. What did you use?
- Where do your PDFs live right now? How many are there?
- When did you last need to find where a claim originally came from?
- What do you use Zotero for, and what do you do outside it?

Do not describe the product until the end. If you describe it first, people are polite and you
learn nothing.

**Deliverable:** one page of notes per person, and a short summary of what surprised you.

**Gate:** if fewer than 4 of 10 people describe a moment where this would have helped, change the
product before building it. That is a cheap and valuable outcome, not a failure.

---

### Phase 1 — The core graph · 15 Sept – 14 Nov 2026 → **v0.1**

Milestones **M0 + M1**. Ingest by DOI or arXiv ID, expand references from OpenAlex with a node
budget, deduplicate on OpenAlex ID, and export GraphML. Ships as cross-compiled binaries for
Windows, macOS and Linux — set up GoReleaser in week one, not week eight.

**Success:** on 20 real papers drawn from 5 different fields, expansion produces a clean graph
with zero duplicate works and no crashes. Open the export in Gephi and it looks right.

**Gate:** if deduplication is still producing duplicates at the end of this phase, stop and fix it
before anything else. A graph that quietly rots is worse than no graph.

---

### Phase 2 — MCP and first users · 15 Nov – 5 Dec 2026 → **v0.2**

Milestone **M2**, plus the first public release. The MCP server is cheap to build because the
logic already exists, and it is the part no competitor has — so it is what you lead with.

Also this phase: publish binaries to GitHub Releases, write a real README with a 30-second demo
recording, and put it in front of people (see §4). A download-and-run binary is a materially
easier ask than any package manager — lean on that.

**Success:** 10 people install it. **3 of them use it more than once.** The second number is the
only one that matters.

**Gate:** if nobody outside you uses it twice within three weeks of launch, do not proceed to
Phase 3 on schedule. Go back to those users and find out what they actually wanted. Building four
more months of features on an unused base is the main way this project fails.

---

### Phase 3 — Papers on disk · 6 Dec 2026 – 20 Jan 2027 → **v0.3**

Milestone **M3**. Open-access PDF fetching, content-addressed storage, text extraction, chunking,
keyword search — **and citation context sentences**, captured here because the text is already
parsed.

Also: Zotero import. It is the cheapest way to give a new user a full library on day one instead
of an empty screen, and empty screens are why tools get uninstalled.

**Success:** someone who is not you adds 50 papers and finds a passage by keyword.

**Note:** this phase spans exams *and* carries the PDF-extraction bake-off (ADR-009), the weakest
part of the Go choice. Run that bake-off in the first week of the phase, not the last.

---

### Phase 4 — Retrieval and answers · 21 Jan – 7 Mar 2027 → **v0.4**

Milestone **M4**. Ollama embeddings, sqlite-vec search, hybrid retrieval, answers with citations.
The first-run experience must detect a missing Ollama and explain what still works — failing
confusingly here would undo the distribution advantage that motivated choosing Go.

**Success:** build a test set of 20 questions where you already know the correct answer and which
paper contains it. Hit the right paper in the top 3 results at least 15 times out of 20. Write
this test set *before* you build the retriever, or you will grade yourself generously.

**Gate:** if hybrid retrieval does not beat plain vector search on that test set, the graph is not
earning its place. Fix edge weighting before adding features.

---

### Phase 5 — The web interface · 8 Mar – 5 Apr 2027 → **v0.5 beta**

Milestone **M5**. Running the binary starts a local server and opens a browser. Graph view,
search, reader, notes. Shorter than originally planned: `//go:embed` compiles the SPA into the
binary, so there is nothing to package or serve separately.

**Success:** three people who cannot code install it and add a paper **without messaging you for
help**. Watch one of them do it over a call and say nothing for ten minutes. It will be painful
and it is the most useful hour of the whole project.

---

### Phase 6 — Claim genealogy · 6 Apr – 23 May 2027 → **v1.0**

Milestone **M6**. Citation intent classification, weighted edges, and tracing a claim back to its
origin.

**Success:** a demo where a widely repeated number traces back to one small old study. Record it.
That recording is your launch.

---

## 4. Release and distribution

Open source projects die from having no users, not from bad code. Treat distribution as real work
with real hours, not something you do after.

**Where the first users are, in order of likely payoff:**

1. **Your own department.** Postgrads at Moratuwa. Ten minutes of asking beats a month of posting.
2. **MCP server directories and awesome-lists.** Small audience, but exactly the people who will
   try a new tool and file good issues.
3. **Zotero forums.** If Zotero import works, this is a receptive, specific audience.
4. **Aaron Tay's blog and newsletter.** He reviews literature-mapping tools seriously and his
   readers are librarians and researchers who adopt them.
5. **Show HN**, once the demo is genuinely good. One shot; do not spend it on v0.2.
6. **r/PhD, r/AskAcademia, academic Bluesky and Mastodon.** Post the claim-genealogy demo, not a
   feature list.

**Release discipline:**

- Tag every phase. Ship **cross-compiled binaries** at v0.1, not just at v1.0. "Download and run"
  is your single biggest advantage over every competing tool — use it from the first release.
- Every release note answers one question: what can I do now that I could not do last month?
- A 30-second screen recording in the README beats three paragraphs of description.

---

## 5. Metrics

**Watch these:**

| Metric | Why |
| --- | --- |
| Weekly active users | The only real signal. 20 real users beats 500 stars |
| Papers added per user after week one | Are they building a library, or did they try it once? |
| Retrieval accuracy on your test set | Guards against the graph being decorative |
| Issues opened by people you do not know | Proof that strangers are actually running it |

**Ignore these:** GitHub stars, PyPI download counts (mostly CI bots and mirrors), and social
media impressions. They feel like progress and are not.

---

## 6. Risk register

| Risk | Severity | What to do about it |
| --- | --- | --- |
| Nobody wants it | **High** | Phase 0 validation, and the hard gate after Phase 2 |
| Scope is large for one person | **High** | Every phase ships something usable, so an interruption leaves a working tool rather than half of one |
| Coursework and exams stall momentum | **High** | Dates already assume December and April are half-speed. Do not add features to recover time — cut them |
| Open-access coverage disappoints users | Medium | Show OA status on every node from the first release, so it never surprises anyone |
| Citation edges are topically noisy | Medium | Edge weighting is a Phase 4 requirement, with the test set to prove it works |
| `sqlite-vec` is pre-1.0 | Medium | Pin the exact version, keep all calls behind one wrapper module |
| Dependency licences force your hand | Medium | Check every licence before adding it. Some PDF libraries are AGPL |
| Learning Go while shipping | Medium | Two weeks are budgeted in Phase 1. If week 4 is not done by 12 Oct, cut scope rather than extend |
| Go PDF extraction is weak | Medium | ADR-009 bake-off in the first week of Phase 3. Bundling `pdftotext` is the fallback |
| Ollama install deters users | Medium | Degrade to keyword plus graph and say so clearly. If users cite it as the reason they quit, revisit pure-Go ONNX |
| Solo maintainer burnout | Medium | Write the contributing guide early. Label good first issues from v0.2 |

---

## 7. Decision points

Three moments where you should be willing to change direction.

**After Phase 0 (mid-Sept).** If the interviews do not find real pain, change the product. The
cheapest pivot you will ever make.

**After Phase 2 (early Dec).** If no stranger uses it twice, stop adding features. Go back to the
people who tried it and find the narrower thing they actually wanted. A tool that does one thing
people need beats a platform nobody opens.

**After Phase 4 (early Mar).** If retrieval does not beat plain search on your own test set, the
graph is decoration. Either fix the edge weighting or accept that this is a good citation graph
tool without the Q&A layer, and ship that instead. That is still a real product.

---

## 8. What v1.0 means

Filiation is version 1.0 when:

- A researcher who cannot code downloads one binary, runs it, and adds their first paper unaided.
- It runs entirely free, with no API key, for its core features. Ollama is optional, and its
  absence is explained rather than fatal.
- Answers cite real papers in the user's own library, and the citations are correct.
- It can trace at least one real claim back to its origin, demonstrably.
- At least 20 people who are not you use it in a given week.

Anything less than the last point is a good portfolio project. All five is a product.
