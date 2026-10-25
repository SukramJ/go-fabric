# A fixture register for script/aheadcheck

Text before the section is not read.

| Entry | marker file | marker text |
| --- | --- | --- |
| BD-Fixture-OutsideTheSection | `src/None.ts` | `"never read"` |

## Ahead of matter.js — register

The column contract stands here in the real file.

| Entry | matter.js marker | marker file | marker text | Behaviour source |
| --- | --- | --- | --- | --- |
| BD-Fixture-Absence | the server adds nothing (absence: the empty class body) | `src/FixtureServer.ts` | `"export class FixtureServer extends FixtureBehavior {}"` | the specification |
| BD-Fixture-TwoFiles | two servers (absence, presence) | `src/FixtureServer.ts`<br>`src/Other.ts` | `"export class FixtureServer"`<br>`"left to the device.\n"` | the specification |
| BD-Fixture-Class | a class body (absence: the whole class) | `src/Multi.ts` | `class Multi` | the specification |
| BD-Fixture-Todo | not recorded | not recorded | not recorded | connectedhomeip |

## The next section

| Entry | marker file | marker text |
| --- | --- | --- |
| BD-Fixture-AfterTheSection | `src/None.ts` | `"never read"` |
