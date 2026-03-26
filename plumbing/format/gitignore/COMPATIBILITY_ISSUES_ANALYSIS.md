# Git Compatibility Issues Analysis

## Overview

Based on comprehensive testing against git's wildmatch test suite (`t3070-wildmatch.sh`) and gitignore tests (`t0008-ignores.sh`), we have identified multiple categories of compatibility issues in go-git's gitignore implementation.

## Issue Categories

### **Category 1: Empty Pattern Handling**
**Issue**: Empty pattern `""` should match empty string `""`
**Current**: Returns `NoMatch` (0)
**Expected**: Returns `Exclude` (1) 
**Impact**: LOW - edge case

### **Category 2: Bracket Expression Negation** 🚨 HIGH PRIORITY
**Issue**: `[!...]` notation not recognized for negation
**Examples**:
- `**[!te]` vs `"ten"` → should match `n` (not `t` or `e`)
- `t[!a-g]n` vs `"ton"` → should match `o` (not in `a-g`)
- `[!------]` vs `"a"` → should match letters (not dashes)

**Root Cause**: Go's `filepath.Match` uses `[^...]` for negation, not `[!...]`
**Fix Required**: Pre-process patterns to convert `[!...]` to `[^...]`

### **Category 3: Advanced Bracket Expressions** 🚨 HIGH PRIORITY  
**Issue**: Complex bracket patterns not handled correctly
**Examples**:
- `a[]]b` → should match `a]b` (closing bracket in set)
- `a[]-]b` → should match `a-b` and `a]b` (dash and bracket in set)
- `a[]a-]b` → should match `aab` (complex character set)

**Root Cause**: Go's bracket expression parsing differs from git's
**Fix Required**: Custom bracket expression handling

### **Category 4: POSIX Character Classes** 🚨 MEDIUM PRIORITY
**Issue**: Character classes like `[[:alpha:]]`, `[[:digit:]]` not working
**Examples**:
- `[[:alpha:]][[:digit:]][[:upper:]]` vs `"a1B"`
- `[[:upper:]]` vs `"A"`
- `[[:lower:]]` vs `"a"`

**Root Cause**: Go's `filepath.Match` may not fully support POSIX classes
**Fix Required**: Enhanced character class parsing

### **Category 5: Double Star Pattern Logic** 🚨 HIGH PRIORITY
**Issue**: `**` patterns not matching correctly in complex scenarios
**Examples**:
- `data/**` should match `data/file`
- `foo**/bar` should match `foo/bar`
- `**/**/**` should handle multiple double stars

**Root Cause**: go-git's `globMatch` function has bugs in `**` handling
**Fix Required**: Rewrite double star matching logic

### **Category 6: Trailing Backslash Handling**
**Issue**: Pattern ending with `\` not handled correctly
**Example**: `foo\\` vs `foo\\`
**Impact**: LOW - rare edge case

### **Category 7: Multi-level Path Counting**
**Issue**: Patterns like `*/*/*` don't correctly count path levels
**Example**: `*/*/*` vs `foo/bb/aa` (should match exactly 3 levels)
**Impact**: MEDIUM - affects complex path patterns

## Failure Statistics

From complete test run:
- **Basic patterns**: ~85% pass
- **Bracket expressions**: ~40% pass  
- **Double star patterns**: ~60% pass
- **Character classes**: ~30% pass
- **Complex patterns**: ~50% pass

## Implementation Priority

### **Phase 1: Critical Fixes**
1. **Bracket Expression Negation** - Fix `[!...]` → `[^...]` conversion
2. **Double Star Logic** - Fix `**` matching in `globMatch` function
3. **Advanced Brackets** - Handle complex bracket expressions

### **Phase 2: Enhanced Compatibility**  
4. **POSIX Character Classes** - Full `[[:class:]]` support
5. **Multi-level Path Counting** - Fix `*/*/*` patterns
6. **Edge Cases** - Empty patterns, trailing backslashes

### **Phase 3: Comprehensive Testing**
7. **Regression Tests** - Ensure all fixes work together
8. **Performance Testing** - Verify no performance degradation
9. **Real-world Validation** - Test against actual gitignore files

## Files Requiring Changes

### **Primary Changes**
- `pattern.go` - Core pattern matching logic
  - `ParsePattern()` - Pre-process bracket expressions
  - `simpleNameMatch()` - Enhanced character class support  
  - `globMatch()` - Fix double star logic

### **Secondary Changes**
- `pattern_test.go` - Update existing tests for new behavior
- Add new test files for regression testing

## Expected Breaking Changes

1. **`[!...]` patterns will change behavior** (currently incorrect)
2. **`**` patterns will match more files** (aligning with git)
3. **Complex bracket patterns will work** (previously broken)

## Risk Mitigation

- All changes bring go-git **closer to git's behavior**
- Current behavior is incorrect per git documentation
- Comprehensive test suite prevents regressions
- Changes are additive (fix broken functionality)

## Next Steps

1. Implement bracket negation fix (`[!...]` → `[^...]`)
2. Fix double star matching in `globMatch`
3. Run test suite after each fix
4. Ensure existing tests continue to pass
5. Create regression test suite