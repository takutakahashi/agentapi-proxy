import { describe, it, expect } from 'vitest';

// Import the functions to test them in isolation
// We'll extract them to a utility file first for testing
function isValidSessionMessageResponse(response: unknown): response is { messages: unknown[] } {
  return (
    response !== null &&
    typeof response === 'object' &&
    'messages' in response &&
    Array.isArray((response as { messages: unknown[] }).messages)
  );
}

function convertSessionMessageId(id: string | number, fallbackId: number): number {
  // Convert to string safely
  const stringId = String(id);
  
  // Handle empty string
  if (!stringId || stringId.trim() === '') {
    return fallbackId;
  }
  
  // Try to parse as number
  const parsed = parseInt(stringId, 10);
  if (!isNaN(parsed) && parsed >= 0) {
    return parsed;
  }
  
  // For negative numbers or non-numeric strings, create a stable hash
  // This ensures the same string always produces the same number
  let hash = 0;
  for (let i = 0; i < stringId.length; i++) {
    const char = stringId.charCodeAt(i);
    hash = ((hash << 5) - hash) + char;
    hash = hash & hash; // Convert to 32-bit integer
  }
  
  // Ensure positive number and use fallback if hash is problematic
  const hashId = Math.abs(hash);
  return hashId > 0 ? hashId : fallbackId;
}

describe('AgentAPIChat utility functions', () => {
  describe('isValidSessionMessageResponse', () => {
    it('should return true for valid response', () => {
      const validResponse = { messages: [], total: 0 };
      expect(isValidSessionMessageResponse(validResponse)).toBe(true);
    });

    it('should return true for response with messages array', () => {
      const validResponse = { messages: [{ id: '1', content: 'test' }] };
      expect(isValidSessionMessageResponse(validResponse)).toBe(true);
    });

    it('should return false for null response', () => {
      expect(isValidSessionMessageResponse(null)).toBe(false);
    });

    it('should return false for undefined response', () => {
      expect(isValidSessionMessageResponse(undefined)).toBe(false);
    });

    it('should return false for response without messages', () => {
      const invalidResponse = { total: 0 };
      expect(isValidSessionMessageResponse(invalidResponse)).toBe(false);
    });

    it('should return false for response with non-array messages', () => {
      const invalidResponse = { messages: null };
      expect(isValidSessionMessageResponse(invalidResponse)).toBe(false);
    });

    it('should return false for response with string messages', () => {
      const invalidResponse = { messages: 'not an array' };
      expect(isValidSessionMessageResponse(invalidResponse)).toBe(false);
    });
  });

  describe('convertSessionMessageId', () => {
    it('should convert numeric string to number', () => {
      expect(convertSessionMessageId('123', 999)).toBe(123);
    });

    it('should handle zero', () => {
      expect(convertSessionMessageId('0', 999)).toBe(0);
    });

    it('should create hash for negative numbers (not use fallback)', () => {
      const result = convertSessionMessageId('-1', 999);
      expect(result).not.toBe(999);
      expect(result).toBeGreaterThan(0);
      // Should be consistent
      expect(convertSessionMessageId('-1', 999)).toBe(result);
    });

    it('should create stable hash for non-numeric strings', () => {
      const hash1 = convertSessionMessageId('abc123', 999);
      const hash2 = convertSessionMessageId('abc123', 999);
      expect(hash1).toBe(hash2);
      expect(hash1).toBeGreaterThan(0);
    });

    it('should create different hashes for different strings', () => {
      const hash1 = convertSessionMessageId('test1', 999);
      const hash2 = convertSessionMessageId('test2', 999);
      expect(hash1).not.toBe(hash2);
    });

    it('should use fallback for empty string if hash is 0', () => {
      // Empty string should use fallback
      const result = convertSessionMessageId('', 999);
      expect(result).toBe(999);
    });

    it('should handle very long strings', () => {
      const longString = 'a'.repeat(1000);
      const result = convertSessionMessageId(longString, 999);
      expect(typeof result).toBe('number');
      expect(result).toBeGreaterThan(0);
    });

    it('should handle special characters', () => {
      const result = convertSessionMessageId('abc-123_xyz@domain.com', 999);
      expect(typeof result).toBe('number');
      expect(result).toBeGreaterThan(0);
    });
  });
});