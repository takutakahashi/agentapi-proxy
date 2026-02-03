# Task Completion Checklist

## Before Committing Code

1. **Run linting**
   ```bash
   make lint
   ```
   Ensure no linting errors exist.

2. **Run tests**
   ```bash
   make test
   ```
   All tests must pass.

3. **Format code**
   ```bash
   make gofmt
   ```
   Code must be properly formatted.

## When Adding/Modifying API Endpoints

1. **Update OpenAPI specification**
   - File: `spec/openapi.json`
   - Add new endpoints with complete request/response schemas
   - Update existing endpoints if modified
   - Add/update schema definitions in `components/schemas`
   - Add tags if necessary

2. **Test the changes**
   - Write unit tests for controllers
   - Run integration tests if applicable

## Git Workflow

1. **Never push to main branch directly**
   - Always create a feature branch
   - Name: `feature/<descriptive-name>`

2. **Commit with meaningful messages**
   - Follow conventional commit format when possible
   - Include context about what changed and why

3. **Create Pull Request**
   - Provide clear description of changes
   - Reference related issues if any
   - Wait for review before merging

## After Task Completion

1. **Verify all changes are committed**
2. **Push to feature branch**
3. **Create PR to main**
4. **Notify user of completion**