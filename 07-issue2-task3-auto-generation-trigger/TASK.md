# Issue 2 / Task 3: generate only after all signers sign

**Important:** These are copies of the relevant files from your uploaded project, organized by task for review. They are not 12 standalone applications. Some files repeat because multiple tasks modify the same shared project file. Copy/merge changes into the original project carefully; do not overwrite a later task with an earlier snapshot.

## Files involved

- `backend/internal/handlers/signing.go`
- `backend/internal/services/finalize.go`
- `backend/internal/services/stamper.go`

## Suggested commit message
```bash
git commit -m "feat(pdf): trigger generation after all signers complete"
```

## Git workflow
```bash
git status
git add <only-files-for-this-task>
git commit -m "feat(pdf): trigger generation after all signers complete"
git push origin <your-branch>
```
