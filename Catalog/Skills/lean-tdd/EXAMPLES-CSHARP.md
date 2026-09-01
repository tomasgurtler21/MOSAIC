# Lean TDD Examples - C# / MSTest

Detailed code examples for Lean TDD principles. Reference from [SKILL.md](SKILL.md).

---

## RED Phase Examples

### Example 1: Valid RED with NotImplementedException

```csharp
// Contract (created by TestCreator)
public interface IOrderCalculator
{
    decimal CalculateTotal(Order order);
}

// Stub implementation (minimal to compile)
public class OrderCalculator : IOrderCalculator
{
    public decimal CalculateTotal(Order order)
    {
        throw new NotImplementedException();
    }
}

// Test
[TestMethod]
public void CalculateTotal_Should_Sum_Item_Prices()
{
    // Arrange
    var calculator = new OrderCalculator();
    var order = new Order 
    { 
        Items = new List<Item> 
        { 
            new Item { Price = 10 }, 
            new Item { Price = 20 } 
        } 
    };

    // Act
    var result = calculator.CalculateTotal(order);

    // Assert
    Assert.AreEqual(30m, result);
}
```

**Result:** `NotImplementedException` thrown → **Valid RED** [PASS]

The test compiles, runs, and fails. This is acceptable RED even though the assertion never executes.

---

### Example 2: Invalid RED - Compilation Error

```csharp
// Test written BEFORE interface exists
[TestMethod]
public void CalculateTotal_Should_Sum_Item_Prices()
{
    var calculator = new OrderCalculator();  // ERROR: Type 'OrderCalculator' not found
    // ...
}
```

**Result:** Compilation error → **NOT valid RED** [FAIL]

Create the interface/stub first, THEN claim RED.

---

### Example 3: Valid RED with Assertion Failure (Ideal)

```csharp
// Stub implementation returns wrong value
public class OrderCalculator : IOrderCalculator
{
    public decimal CalculateTotal(Order order)
    {
        return 0m;  // Stub returns zero
    }
}

// Test
[TestMethod]
public void CalculateTotal_Should_Sum_Item_Prices()
{
    // Arrange
    var calculator = new OrderCalculator();
    var order = new Order { Items = new List<Item> { new Item { Price = 30 } } };

    // Act
    var result = calculator.CalculateTotal(order);

    // Assert
    Assert.AreEqual(30m, result);  // Fails: Expected 30, Actual 0
}
```

**Result:** Assertion fails with clear message → **Ideal RED** [PASS]

---

## Behavioral Testing Examples

### Example 4: BAD - Testing Compiler-Enforced Structure

```csharp
// BAD: Testing that properties exist and have correct types
[TestClass]
public class UserTests
{
    [TestMethod]
    public void User_Should_Have_Id_Property()
    {
        var user = new User();
        Assert.IsInstanceOfType(user.Id, typeof(int));  // Compiler enforces this
    }

    [TestMethod]
    public void User_Should_Have_Email_Property()
    {
        var user = new User();
        Assert.IsNotNull(user.Email);  // May fail for wrong reason (default is null)
    }

    [TestMethod]
    public void User_Email_Should_Be_String()
    {
        var user = new User { Email = "test@test.com" };
        Assert.IsInstanceOfType(user.Email, typeof(string));  // Always true by definition
    }
}
```

**Why these are wasteful:**
- The compiler already ensures `Id` is `int` and `Email` is `string`
- These tests add no value - they can never catch a real bug
- They inflate test count without improving confidence

---

### Example 5: GOOD - Testing Business Logic

```csharp
// GOOD: Testing actual behavior and decisions
[TestClass]
public class UserValidatorTests
{
    private UserValidator _validator;

    [TestInitialize]
    public void Setup()
    {
        _validator = new UserValidator();
    }

    [TestMethod]
    public void Validate_When_Email_Missing_Should_Return_Invalid()
    {
        // Arrange
        var user = new User { Name = "John", Email = null };

        // Act
        var result = _validator.Validate(user);

        // Assert
        Assert.IsFalse(result.IsValid);
        Assert.IsTrue(result.Errors.Any(e => e.Contains("Email")));
    }

    [TestMethod]
    public void Validate_When_Email_Invalid_Format_Should_Return_Invalid()
    {
        // Arrange
        var user = new User { Name = "John", Email = "not-an-email" };

        // Act
        var result = _validator.Validate(user);

        // Assert
        Assert.IsFalse(result.IsValid);
    }

    [TestMethod]
    public void Validate_When_All_Fields_Valid_Should_Return_Valid()
    {
        // Arrange
        var user = new User { Name = "John", Email = "john@example.com" };

        // Act
        var result = _validator.Validate(user);

        // Assert
        Assert.IsTrue(result.IsValid);
    }
}
```

**Why these are valuable:**
- Test actual validation LOGIC (decisions)
- Could catch real bugs (wrong regex, missing null check)
- Document expected behavior

---

## Exception Testing Examples

### Example 6: BAD - Exact Message Matching

```csharp
// BAD: Coupled to exact exception message wording
[TestMethod]
public void GetUser_When_NotFound_Should_Have_Specific_Message()
{
    // Arrange
    _mockRepo.Setup(r => r.FindById(123)).Returns((User)null);

    // Act & Assert
    var ex = Assert.ThrowsException<UserNotFoundException>(
        () => _service.GetUser(123));
    
    // This assertion is BRITTLE
    Assert.AreEqual("User with ID 123 was not found in the database", ex.Message);
}
```

**Why this breaks:**
- Developer improves message to "User 123 not found" → test fails
- Localization changes message → test fails
- Added context "User with ID 123 was not found in database 'Users'" → test fails

---

### Example 7: GOOD - Type and Context Presence

```csharp
// GOOD: Tests the contract (type) and traceability (context present)
[TestMethod]
public void GetUser_When_NotFound_Should_Throw_UserNotFoundException()
{
    // Arrange
    _mockRepo.Setup(r => r.FindById(123)).Returns((User)null);

    // Act & Assert
    Assert.ThrowsException<UserNotFoundException>(() => _service.GetUser(123));
}

[TestMethod]
public void GetUser_When_NotFound_Exception_Should_Be_Traceable()
{
    // Arrange
    var userId = 456;
    _mockRepo.Setup(r => r.FindById(userId)).Returns((User)null);

    // Act
    var ex = Assert.ThrowsException<UserNotFoundException>(
        () => _service.GetUser(userId));

    // Assert - verify context IS PRESENT, not exact format
    StringAssert.Contains(ex.Message, userId.ToString());
}
```

**Why this is robust:**
- Exception type is stable API contract
- `Contains` check ensures traceability without format coupling
- Message can be improved without breaking test

---

### Example 8: Edge Case - When to Test Exception Properties

```csharp
// ACCEPTABLE: When the property IS the behavior being tested
// Scenario: Exception must carry error code for API response mapping

[TestMethod]
public void ProcessPayment_When_Declined_Should_Include_DeclineCode()
{
    // Arrange
    _mockGateway.Setup(g => g.Charge(It.IsAny<Payment>()))
                .Throws(new PaymentDeclinedException("declined", "INSUFFICIENT_FUNDS"));

    // Act
    var ex = Assert.ThrowsException<PaymentException>(
        () => _service.ProcessPayment(new Payment { Amount = 100 }));

    // Assert - ErrorCode IS the contract (used by API layer)
    Assert.AreEqual("INSUFFICIENT_FUNDS", ex.ErrorCode);
}
```

**When property testing is acceptable:**
- The property is part of the public API contract
- Downstream code depends on specific values (error codes, status enums)
- The property represents a DECISION, not just context

---

## Mocking Examples

### Example 9: BAD - Verifying Implementation Details

```csharp
// BAD: Tests HOW the method works, not WHAT it does
[TestMethod]
public void CreateUser_Should_Call_Repository_And_EventPublisher()
{
    // Arrange
    var dto = new CreateUserDto { Name = "John", Email = "john@test.com" };

    // Act
    _service.CreateUser(dto);

    // Assert - testing implementation details
    _mockRepo.Verify(r => r.Save(It.IsAny<User>()), Times.Once);
    _mockEventPublisher.Verify(p => p.Publish(It.IsAny<UserCreatedEvent>()), Times.Once);
    _mockLogger.Verify(l => l.LogInformation(It.IsAny<string>()), Times.AtLeastOnce);
}
```

**Problems:**
- Test breaks if you add caching (Save called conditionally)
- Test breaks if you batch events (Publish called differently)
- Test breaks if you change logging strategy
- Doesn't verify the USER was actually created correctly

---

### Example 10: GOOD - Testing Outcomes

```csharp
// GOOD: Tests WHAT the method produces, not HOW
[TestMethod]
public void CreateUser_Should_Return_User_With_Generated_Id()
{
    // Arrange
    var dto = new CreateUserDto { Name = "John", Email = "john@test.com" };
    _mockRepo.Setup(r => r.Save(It.IsAny<User>()))
             .Callback<User>(u => u.Id = 42)
             .Returns((User u) => u);

    // Act
    var result = _service.CreateUser(dto);

    // Assert - tests the OUTCOME
    Assert.IsNotNull(result);
    Assert.AreEqual(42, result.Id);
    Assert.AreEqual("John", result.Name);
}

[TestMethod]
public void CreateUser_Should_Return_User_With_Correct_Data()
{
    // Arrange
    var dto = new CreateUserDto { Name = "Jane", Email = "jane@test.com" };
    User savedUser = null;
    _mockRepo.Setup(r => r.Save(It.IsAny<User>()))
             .Callback<User>(u => savedUser = u)
             .Returns((User u) => u);

    // Act
    var result = _service.CreateUser(dto);

    // Assert - verify the data, not the method calls
    Assert.AreEqual("Jane", result.Name);
    Assert.AreEqual("jane@test.com", result.Email);
}
```

---

### Example 11: Acceptable Verify - Side Effect IS the Behavior

```csharp
// ACCEPTABLE: When the side effect IS the behavior being tested
// Scenario: SendWelcomeEmail is the primary purpose of this method

[TestMethod]
public void SendWelcomeEmail_Should_Send_To_User_Email()
{
    // Arrange
    var user = new User { Email = "test@example.com", Name = "John" };

    // Act
    _service.SendWelcomeEmail(user);

    // Assert - sending IS the behavior (no return value to check)
    _mockEmailSender.Verify(
        e => e.Send(It.Is<Email>(email => email.To == "test@example.com")), 
        Times.Once);
}
```

**When Verify is acceptable:**
- The method's PURPOSE is the side effect (sending email, writing to queue)
- There's no return value to verify
- You're verifying the CONTRACT (what was sent), not internal calls

---

## AAA Pattern Examples

### Example 12: Clean AAA Structure

```csharp
[TestMethod]
public void CalculateShipping_When_Order_Over_100_Should_Be_Free()
{
    // Arrange
    var order = new Order 
    { 
        Items = new List<Item> 
        { 
            new Item { Price = 75 },
            new Item { Price = 50 }
        }
    };
    var calculator = new ShippingCalculator();

    // Act
    var shippingCost = calculator.Calculate(order);

    // Assert
    Assert.AreEqual(0m, shippingCost);
}
```

### Example 13: BAD - Multiple Acts

```csharp
// BAD: Two acts, unclear which one failed
[TestMethod]
public void UserService_Create_And_Delete_Should_Work()
{
    // Arrange
    var dto = new CreateUserDto { Name = "John" };

    // Act 1
    var created = _service.CreateUser(dto);
    
    // Act 2
    _service.DeleteUser(created.Id);
    
    // Assert
    Assert.ThrowsException<UserNotFoundException>(() => _service.GetUser(created.Id));
}
```

**Fix:** Split into two tests - one for Create, one for Delete.

---

## Summary Table

| Example | Pattern | Verdict |
|---------|---------|---------|
| NotImplementedException | RED Phase | [PASS] Valid RED |
| Compile error | RED Phase | [FAIL] Not RED yet |
| Property existence test | Behavioral | [FAIL] Wasteful |
| Validation logic test | Behavioral | [PASS] Valuable |
| Exact message match | Exception | [FAIL] Brittle |
| Contains check | Exception | [PASS] Robust |
| Verify call count | Mocking | [FAIL] Implementation detail |
| Test return value | Mocking | [PASS] Tests outcome |
| Verify for fire-and-forget | Mocking | [WARN] Acceptable when side effect IS behavior |
