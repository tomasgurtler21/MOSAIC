---
name: lean-tdd
version: 1.0.0
description: Lean TDD practices that eliminate wasteful testing patterns. Use when writing tests, reviewing test code, or validating RED/GREEN phases. Covers valid RED phase definition, behavioral testing principles, exception assertions, and mocking guidelines. Language-agnostic principles with C# examples.
---

> **Read This Entire File:** This skill file must be read in full before applying any guidance. If your file reading tool has a line limit (e.g., 80 lines by default), use explicit limit/offset parameters to read beyond that. Keep reading until you reach the `END OF SKILL` marker at the bottom of this file. Do not proceed until you have done so.

# Lean TDD

Lean TDD eliminates wasteful testing practices while preserving TDD's core value: tests as executable specifications.

> **Note:** Examples are shown in C#/MSTest syntax. The principles apply universally to any language and testing framework.

## Quick Start: Three Core Rules

1. **RED = Compiles + Fails** - Test must compile. Runtime failure (including `NotImplementedException`) counts as RED.
2. **Test Behavior, Not Structure** - Don't test what the compiler guarantees.
3. **Test Intent, Not Wording** - For exceptions, verify type and context presence, not exact messages.

---

## The RED Phase

### What Counts as Valid RED

| Scenario | Valid RED? | Reason |
|----------|------------|--------|
| Test compiles, assertion fails | ✅ YES | Ideal RED - behavioral failure |
| Test compiles, `NotImplementedException` thrown | ✅ YES | Acceptable RED - implementation absent |
| Test compiles, `NullReferenceException` during setup | ✅ YES | Valid RED - runtime failure |
| Test does not compile (missing class/method) | ❌ NO | Still in "write test" phase |
| Test compiles but you haven't run it | ❌ NO | RED requires execution |

### The Critical Insight

**GREEN means the assertion PASSES, not just "no exception."**

If your implementation catches exceptions or returns default values, a test that previously threw `NotImplementedException` might now "pass" without actually verifying behavior. Always ensure GREEN comes from assertion success.

### RED Phase Verification Checklist

Before claiming RED:
- [ ] Test file compiles without errors
- [ ] Test executes (run it!)
- [ ] Test fails (check the failure reason)
- [ ] Failure is expected given no/stub implementation

---

## Test Behavior, Not Structure

### What NOT to Test

These are **compiler-enforced** or **static analysis concerns**:

```csharp
// BAD: Testing that a property exists
[TestMethod]
public void User_Should_Have_Email_Property()
{
    var user = new User();
    Assert.IsNotNull(user.Email); // Compiler already enforces this exists
}

// BAD: Testing return type
[TestMethod]
public void GetUser_Should_Return_User_Type()
{
    var result = _service.GetUser(1);
    Assert.IsInstanceOfType(result, typeof(User)); // Compiler enforces return type
}

// BAD: Testing that constructor sets properties
[TestMethod]
public void User_Constructor_Should_Set_Name()
{
    var user = new User("John");
    Assert.AreEqual("John", user.Name); // This IS behavior, but trivial
}
```

### What TO Test

Test **decisions**, **calculations**, and **business rules**:

```csharp
// GOOD: Testing a calculation
[TestMethod]
public void CalculateDiscount_When_PremiumMember_Should_Apply_20_Percent()
{
    var order = new Order { Total = 100, MembershipLevel = Level.Premium };
    var result = _calculator.CalculateDiscount(order);
    Assert.AreEqual(20m, result);
}

// GOOD: Testing a decision
[TestMethod]
public void ValidateOrder_When_Empty_Should_Reject()
{
    var order = new Order { Items = new List<Item>() };
    var result = _validator.Validate(order);
    Assert.IsFalse(result.IsValid);
}

// GOOD: Testing error handling behavior
[TestMethod]
public void GetUser_When_NotFound_Should_Throw_UserNotFoundException()
{
    _mockRepo.Setup(r => r.Find(999)).Returns((User)null);
    Assert.ThrowsException<UserNotFoundException>(() => _service.GetUser(999));
}
```

### The Litmus Test

Ask: **"Could this test fail if the behavior is correct?"**

- If YES → Test is checking implementation details → **Remove or refactor**
- If NO → Test is checking behavior → **Keep**

---

## Exception Testing

### The Problem with Message Matching

Exception messages are **human-readable documentation**, not API contracts. They change for:
- Typo fixes
- Clarity improvements  
- Localization
- Adding context

### WRONG: Exact Message Matching

```csharp
// BRITTLE: Breaks on any wording change
[TestMethod]
public void GetUser_When_NotFound_Should_Have_Correct_Message()
{
    var ex = Assert.ThrowsException<UserNotFoundException>(
        () => _service.GetUser(123));
    
    Assert.AreEqual("User with ID 123 was not found in the database", ex.Message);
    // Fails if message changes to "User 123 not found" or "Cannot find user: 123"
}
```

### WRONG: Testing Exception Property Values

```csharp
// BRITTLE: Testing implementation details
[TestMethod]
public void GetUser_When_NotFound_Should_Have_UserId_Property()
{
    var ex = Assert.ThrowsException<UserNotFoundException>(
        () => _service.GetUser(123));
    
    Assert.AreEqual(123, ex.UserId);  // Why does the test care about this?
}
```

### CORRECT: Test Type and Context Presence

```csharp
// GOOD: Tests exception type (the contract)
[TestMethod]
public void GetUser_When_NotFound_Should_Throw_UserNotFoundException()
{
    Assert.ThrowsException<UserNotFoundException>(() => _service.GetUser(123));
}

// GOOD: Tests that context IS PRESENT for traceability (not exact format)
[TestMethod]
public void GetUser_When_NotFound_Exception_Should_Contain_UserId()
{
    var ex = Assert.ThrowsException<UserNotFoundException>(
        () => _service.GetUser(123));
    
    StringAssert.Contains(ex.Message, "123");  // Verifies traceability, not wording
}
```

### Exception Testing Guidelines

| Approach | Verdict | Reason |
|----------|---------|--------|
| `Assert.ThrowsException<T>()` | ✅ DO | Tests the contract (exception type) |
| `StringAssert.Contains(ex.Message, key)` | ✅ DO | Ensures traceability without coupling to format |
| `Assert.AreEqual(expected, ex.Message)` | ❌ DON'T | Couples test to exact wording |
| `Assert.AreEqual(value, ex.Property)` | ⚠️ RARELY | Only if property IS the behavior being tested |

---

## Mocking Principles

### Mock Interfaces, Not Implementations

```csharp
// GOOD: Mocking an interface
private Mock<IUserRepository> _mockRepo;

[TestInitialize]
public void Setup()
{
    _mockRepo = new Mock<IUserRepository>();
    _service = new UserService(_mockRepo.Object);
}

// BAD: Trying to mock a concrete class
private Mock<UserRepository> _mockRepo;  // Requires virtual methods, signals design issue
```

**If you can't mock it, your design needs refactoring.** Concrete dependencies indicate tight coupling.

### Avoid Verify() - Test Outputs Instead

```csharp
// BAD: Testing HOW (implementation detail)
[TestMethod]
public void CreateUser_Should_Call_Repository_Save()
{
    _service.CreateUser(new UserDto { Name = "John" });
    
    _mockRepo.Verify(r => r.Save(It.IsAny<User>()), Times.Once);  // Tests implementation
}

// GOOD: Testing WHAT (behavior/outcome)
[TestMethod]
public void CreateUser_Should_Return_Created_User_With_Id()
{
    _mockRepo.Setup(r => r.Save(It.IsAny<User>()))
             .Returns((User u) => { u.Id = 42; return u; });
    
    var result = _service.CreateUser(new UserDto { Name = "John" });
    
    Assert.AreEqual(42, result.Id);  // Tests the outcome
}
```

### Mocking Guidelines

| Practice | Verdict |
|----------|---------|
| Mock interfaces only | ✅ DO |
| Test outputs/return values | ✅ DO |
| `Verify()` for call occurrence | ⚠️ AVOID |
| `Verify()` with `Times.Exactly(n)` | ❌ DON'T |
| Multiple mocks in one test | ⚠️ SMELL - consider integration test |

---

## Arrange-Act-Assert Pattern

All tests MUST follow AAA structure with clear visual separation:

```csharp
[TestMethod]
public void MethodName_When_Condition_Should_ExpectedResult()
{
    // Arrange
    var input = new InputDto { Value = "test" };
    _mockDependency.Setup(d => d.Process(It.IsAny<string>())).Returns(true);

    // Act
    var result = _sut.Execute(input);

    // Assert
    Assert.IsTrue(result.Success);
    Assert.AreEqual("processed", result.Status);
}
```

### AAA Rules

1. **One Act per test** - If you have multiple Acts, split into multiple tests
2. **Arrange can use helper methods** - Keep test body readable
3. **Assert one logical concept** - Multiple assertions OK if testing one behavior
4. **Comments are optional but helpful** - `// Arrange`, `// Act`, `// Assert`

---

## Anti-Patterns Quick Reference

| Anti-Pattern | Problem | Solution |
|--------------|---------|----------|
| Claiming RED on compile error | Not TDD - test doesn't exist yet | Ensure test compiles before claiming RED |
| Testing property existence | Compiler enforces this | Test behavior that uses the property |
| Exact exception message match | Breaks on wording changes | Use `StringAssert.Contains()` for key context |
| `Verify()` call counts | Tests implementation, not behavior | Test outputs and return values |
| Mocking concrete classes | Indicates tight coupling | Refactor to depend on interfaces |
| Multiple Acts in one test | Hard to identify which failed | One test, one Act, one logical assertion |
| Tests that pass without implementation | Defeats TDD purpose | Verify test fails first (RED) |

---

## Detailed Examples

For comprehensive code examples covering edge cases, see [EXAMPLES-CSHARP.md](EXAMPLES-CSHARP.md).

---

## When to Apply This Skill

Apply Lean TDD principles when:
- Writing new test cases
- Implementing code to make tests pass
- Reviewing test quality
- Validating RED/GREEN phase transitions

This skill complements but does not replace project-specific testing guidelines.

---

END OF SKILL
