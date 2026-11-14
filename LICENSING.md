# Licensing

Traceline is **open source** under the **GNU Affero General Public License, version 3
(AGPL-3.0)**, and it is **also available under a paid commercial license**. This file explains
what that means in practice and why both exist.

The full license text is in [`LICENSE`](LICENSE). SPDX identifier: `AGPL-3.0-or-later`.

---

## What the AGPL gives you, for free

You may use, study, modify and redistribute Traceline under AGPL-3.0. In particular you may:

- run it internally at your company, for any purpose, including commercial work;
- run it as a CLI, in CI, or inside a container, and keep your own analysis inputs private
  (inputs and reports are your data, not a derivative work of the software);
- read the source, fork it, patch it, and publish your fork — as long as the fork stays under
  the AGPL.

Two obligations come with it:

1. **If you distribute Traceline** (or a modified version, or a binary embedding it), the
   recipient must get the source and the same AGPL rights.
2. **If you offer it as a network service**, AGPL §13 requires you to offer the corresponding
   source — *including your modifications* — to the people using that service.

## What the commercial license is for

A commercial license removes the AGPL's copyleft obligations. Buy it if you want to:

- embed Traceline in a closed-source product or an internal platform you do not intend to
  publish;
- offer it as a hosted/web service without publishing your modifications;
- ship it under terms other than the AGPL, or need a warranty/indemnity statement the AGPL
  explicitly does not provide (AGPL §§15–16).

Pricing is per organization, not per seat. Contact the copyright holder:

**André Luiz Buzzo de Souza** — through the repository's issue tracker or the address on the
GitHub profile [`@andrelbuzzo`](https://github.com/andrelbuzzo).

## Clarifications people ask for

| Question | Answer |
|---|---|
| Can I use it at work for free? | Yes. Internal use, including at a for-profit company, is free under the AGPL. Copyleft triggers on **distribution** and on **offering it as a network service**, not on internal use. |
| Can I sell a service built on it? | Only if you comply with AGPL §13 (publish your modifications and offer the source to your users) — or buy the commercial license. |
| Is this "non-commercial"? | **No.** The AGPL is an OSI-approved open-source license and it explicitly permits commercial use. A license that forbade commercial use would not be open source at all. |
| Do reports I generate belong to me? | Yes. The output of running the tool (`report.html`, `report.pdf`, `result.json`, …) is yours, with no license obligation. |
| Can I contribute a patch? | Yes, under the AGPL. Contributions are accepted under the same license, which also lets the copyright holder relicense them commercially (dual licensing requires this to be possible). |

## Why dual licensing instead of a "no commercial use" license

The project started from a brief that asked for "open source, but nobody may use it
commercially". Those two requirements contradict each other: the Open Source Definition (and
the OSI's own field-of-use rule) forbids a license that discriminates against commercial use.
AGPL-3.0 was chosen because it is genuinely open source, it does permit commercial use, and
its network clause is precisely what makes a paid commercial license worth buying for anyone
who wants to keep their modifications private. See `docs/DECISIONS.md`.
