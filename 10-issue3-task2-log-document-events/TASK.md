# Issue 3 / Task 2: log upload, boxes, email and signing events

**Important:** These are copies of the relevant files from your uploaded project, organized by task for review. They are not 12 standalone applications. Some files repeat because multiple tasks modify the same shared project file. Copy/merge changes into the original project carefully; do not overwrite a later task with an earlier snapshot.

## Files involved

- `backend/internal/services/audit.go`
- `backend/internal/handlers/auditlog.go`
- `backend/internal/handlers/documents.go`
- `backend/internal/handlers/boxes.go`
- `backend/internal/handlers/signers.go`
- `backend/internal/handlers/signing.go`

## Suggested commit message
```bash
git commit -m "feat(audit): log document activities and signer IP"
```

## Git workflow
```bash
git status
git add <only-files-for-this-task>
git commit -m "feat(audit): log document activities and signer IP"
git push origin <your-branch>
```
