Our goal is to Develop a functional To-Do list web application based on the requirements in PRD.md.



Phase 1: Planning
1. Create a file named PRD.md documenting the MVP 
    - CRUD endpoints (add, check, delete tasks) 
    - Input validation
    - Constraints:
      a. use bootstrap CSS framework for the UI interface.
      b. written using python flask 3.1+, with gunicorn webserver, 
      c. Use Opentelemetry libraries to instrument traces logs and metrics. 
      d. package as a container image.
        - Dockerfile should use multi-stage builds (build stage + runtime stage)
        - container should not run as root
      e. dependencies managed with uv.
      f. set pyenv for this project to 3.12.7
      g. use SQLAlchemy
        - local dev environment can use sqlite
        - production ready should support mysql or postgres
      h. Flask blueprints for API Endpoints
        - endpoints should return a json object with "data" and "error"
        - use `apps/models` for DB models, `app/services` for business logic
      i. tox should be used to run test suite (linting, type checking, validate across multiple python versions above base, code formatting)
   work back and forth with me, starting with your open questions and outline before creating the PRD.md
2. Create a file named TASKLIST.md with atomic, actionalble tasks to fulfill the PRD.
    - collaborate with me and ask questions when creating tasks to get clarification and ensure we are on the same page while designing this project.
   work back and forth with me on each task you come up before writing committing the task to TASKLIST.md

Acceptance Criteria:
- PRD.md has been approved by me
- all tasks in TASKLIST.md have been approved by me

When you need clarification or an approval, output exactly one line in this format:
Tool: question: <your question>
then stop and wait

## Completion Promise
Output <promise>COMPLETE</promise> only when all Acceptance criteria are met and PRD.md and TASKLIST.md exist in the project.
