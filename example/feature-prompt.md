Our goal is to add priorities and categories to the To-Do list web application.

Phase 1:
Planning objectives:
1. Update an MVP Product Requirements Document in PRD.md.
2. Produce an atomic, actionable task plan in TASKLIST-task-priorities-categories.md.

MVP requirements to include:
1. CRUD scope for tasks:
- add task
- check task
- update task
- delete task
2. Input validation rules for all endpoints.
3. UI uses Bootstrap.
4. Backend uses Flask 3.1+ with Gunicorn.
5. Use OpenTelemetry for traces, logs, and metrics.
6. Containerized delivery:
- multi-stage Docker build (build + runtime)
- runtime container must not run as root
7. Dependencies managed by uv.
8. Python version pinned with pyenv to 3.12.7.
9. Data layer uses SQLAlchemy:
- local dev supports SQLite
- production-ready support for MySQL or PostgreSQL
10. API endpoints use Flask blueprints.
11. Endpoint responses always return JSON with keys:
- data
- error
12. Code organization:
- DB models in apps/models
- business logic in app/services
13. tox runs:
- linting
- type checking
- code formatting checks
- multi-version test matrix above base version

Execution sequence:
1. Update PRD.md.
2. Create TASKLIST-task-priorities-categories.md with atomic tasks.


Phase 2: Building (Iterate on this)
1. Read the TASKLIST-task-priorities-categories.md.
2. leverage Test driven development for each task using pytest
3. Select the next incomplete task.
4. Implement the task.
5. Run tests and linting to ensure it works
6. Update TASKLIST-task-priorities-categories.md to make the task as done.
7. Commit changes to git whenever a new task from TASKLIST-task-priorities-categories is completed with a commit message describing the work that was done.

Acceptance Criteria:
- All tasks in TASKLIST-task-priorities-categories.md are marked as done.
- The app has no lint errors
- The app passes all tests.
- build and run the application and ensure end to end tests work with locust testing tool.
- Update README.md on how a new contributor can do additional development and how someone can run this app leveraging this new feature.

## Completion Promise
Output <promise>COMPLETE</promise> only when all acceptance criteria are met.