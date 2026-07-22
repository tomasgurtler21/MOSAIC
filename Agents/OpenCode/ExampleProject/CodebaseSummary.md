# Codebase AI Summary — TaskFlow API

This document provides coding agents with essential information for working in this Node.js TypeScript codebase.

## Project Overview

- **Name**: TaskFlow API
- **Purpose**: REST API for task/project management (teams, tasks, comments, notifications)
- **Framework**: Node.js 20 + Express 4 + TypeScript 5
- **Database**: PostgreSQL 16 via Prisma ORM
- **Authentication**: JWT-based with refresh tokens
- **Architecture**: Layered (routes → controllers → services → repositories)

## Build Commands

```bash
# Install dependencies
npm install

# Build TypeScript
npm run build

# Start development server (with hot reload)
npm run dev

# Start production server
npm start

# Lint
npm run lint

# Type check only
npm run typecheck
```

## Test Commands

```bash
# Run all tests
npm test

# Run single test file
npx jest src/services/__tests__/task.service.test.ts

# Run tests matching pattern
npx jest --testNamePattern="should create task"

# Run tests with coverage
npm run test:coverage

# Run integration tests (requires running database)
npm run test:integration
```

**Test Framework**: Jest with ts-jest, supertest for HTTP testing

## Code Style Guidelines

### Formatting
- **Indentation**: 2 spaces (no tabs)
- **Line length**: Maximum 100 characters
- **Semicolons**: Required
- **Quotes**: Single quotes for strings
- **Trailing commas**: Required in multiline

### Naming Conventions

| Element | Convention | Example |
|---------|------------|---------|
| Files | kebab-case | `task.service.ts`, `auth.middleware.ts` |
| Classes | PascalCase | `TaskService`, `AuthMiddleware` |
| Interfaces | PascalCase (no I prefix) | `TaskCreateInput`, `UserResponse` |
| Functions, variables | camelCase | `createTask`, `userId` |
| Constants | UPPER_SNAKE_CASE | `MAX_PAGE_SIZE`, `JWT_EXPIRY` |
| Enums | PascalCase (members UPPER_SNAKE) | `TaskStatus.IN_PROGRESS` |

### Type Usage
- **Strict TypeScript**: `strict: true` in tsconfig
- **Avoid `any`**: Use `unknown` when type is uncertain
- **Prefer interfaces** over type aliases for object shapes
- **Use Zod** for runtime validation of API inputs

### Error Handling
- Custom `AppError` class with HTTP status codes
- All errors go through centralized error middleware
- Never catch and swallow errors silently
- Use `Result<T>` pattern in services (no throwing in business logic)

## Project Structure

```
src/
├── config/               # Environment config, database connection
├── middleware/            # Auth, validation, error handling, rate limiting
├── routes/               # Express route definitions
├── controllers/          # Request/response handling (thin layer)
├── services/             # Business logic (core layer)
├── repositories/         # Database access via Prisma
├── models/               # Zod schemas and TypeScript interfaces
├── utils/                # Helpers (pagination, hashing, date formatting)
├── jobs/                 # Background jobs (email notifications, cleanup)
├── __tests__/            # Integration tests
│   └── fixtures/         # Test data factories
└── index.ts              # Application entry point

prisma/
├── schema.prisma         # Database schema
└── migrations/           # Migration files
```

## Key Patterns

### Controller Pattern
Controllers are thin — validate input, call service, format response:

```typescript
export const createTask = async (req: Request, res: Response): Promise<void> => {
  const input = taskCreateSchema.parse(req.body);
  const result = await taskService.createTask(input, req.user.id);

  if (!result.ok) {
    throw new AppError(result.error.message, result.error.code);
  }

  res.status(201).json({ data: result.value });
};
```

### Service Pattern
Services contain business logic and return Result types:

```typescript
export class TaskService {
  async createTask(input: TaskCreateInput, userId: string): Promise<Result<Task>> {
    const project = await this.projectRepo.findById(input.projectId);
    if (!project) {
      return err({ message: 'Project not found', code: 404 });
    }

    const isMember = await this.memberRepo.isMember(userId, project.id);
    if (!isMember) {
      return err({ message: 'Not a project member', code: 403 });
    }

    const task = await this.taskRepo.create({ ...input, createdBy: userId });
    await this.notificationService.notifyTaskCreated(task);

    return ok(task);
  }
}
```

### Repository Pattern
Repositories encapsulate Prisma queries:

```typescript
export class TaskRepository {
  async create(data: TaskCreateData): Promise<Task> {
    return this.prisma.task.create({ data, include: { assignee: true } });
  }

  async findByProject(projectId: string, pagination: PaginationInput): Promise<PaginatedResult<Task>> {
    const [tasks, total] = await Promise.all([
      this.prisma.task.findMany({
        where: { projectId },
        skip: pagination.offset,
        take: pagination.limit,
        orderBy: { createdAt: 'desc' },
      }),
      this.prisma.task.count({ where: { projectId } }),
    ]);

    return paginate(tasks, total, pagination);
  }
}
```

### Test Pattern
Tests use factory functions for test data:

```typescript
describe('TaskService', () => {
  let service: TaskService;
  let mockTaskRepo: jest.Mocked<TaskRepository>;
  let mockProjectRepo: jest.Mocked<ProjectRepository>;

  beforeEach(() => {
    mockTaskRepo = createMockTaskRepo();
    mockProjectRepo = createMockProjectRepo();
    service = new TaskService(mockTaskRepo, mockProjectRepo);
  });

  it('should create task when user is project member', async () => {
    mockProjectRepo.findById.mockResolvedValue(projectFactory.build());
    mockMemberRepo.isMember.mockResolvedValue(true);
    mockTaskRepo.create.mockResolvedValue(taskFactory.build());

    const result = await service.createTask(taskCreateInputFactory.build(), 'user-1');

    expect(result.ok).toBe(true);
    expect(mockTaskRepo.create).toHaveBeenCalledOnce();
  });
});
```

## Database Schema (Key Entities)

- **User**: id, email, name, passwordHash, role (ADMIN/MEMBER)
- **Project**: id, name, description, ownerId, createdAt
- **ProjectMember**: userId, projectId, role (OWNER/EDITOR/VIEWER)
- **Task**: id, title, description, status (TODO/IN_PROGRESS/REVIEW/DONE), priority (LOW/MEDIUM/HIGH/URGENT), assigneeId, projectId, dueDate
- **Comment**: id, content, taskId, authorId, createdAt
- **Notification**: id, type, userId, taskId, read, createdAt

## API Endpoints (Partial)

| Method | Path | Description |
|--------|------|-------------|
| POST | `/auth/register` | Register new user |
| POST | `/auth/login` | Login, returns JWT |
| POST | `/auth/refresh` | Refresh access token |
| GET | `/projects` | List user's projects |
| POST | `/projects` | Create project |
| GET | `/projects/:id/tasks` | List tasks in project |
| POST | `/projects/:id/tasks` | Create task |
| PATCH | `/tasks/:id` | Update task |
| POST | `/tasks/:id/comments` | Add comment |
| GET | `/notifications` | List user notifications |
