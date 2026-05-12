# Stagehand <version>

Release date: <YYYY-MM-DD>

## Summary

- <one-line product summary>

## Highlights

- <major user-visible change>
- <major user-visible change>
- <major user-visible change>

## Install

CLI:

```bash
# Download the matching release archive, then:
stagehand --help
```

Python:

```bash
python -m pip install stagehand==<python-version>
```

TypeScript:

```bash
npm install @stagehand/sdk@<version>
```

## Compatibility

- Artifact schema: `v1alpha1`
- Config schema: `v1alpha1`
- Python: `>=3.10`
- Node: see `sdk/typescript/package.json`

## Migration Notes

- <breaking or behavior-changing item>
- <baseline regeneration guidance if needed>

## Verification

- [ ] `npm run ci:all`
- [ ] `npm run package:release`
- [ ] `npm run verify:clean-onboarding`
- [ ] OpenAI verification agents with `OPENAI_API_KEY`
- [ ] Stripe conformance with `STRIPE_SECRET_KEY` when applicable

## Known Limitations

- See `docs/limitations.md`.
