#!/usr/bin/env python3
"""
Comprehensive Redis Server Test Script
Tests all implemented commands across all data structures
"""

import socket
import time
import sys
from typing import List, Optional, Any


class RedisClient:
    """Simple Redis client using RESP protocol"""
    
    def __init__(self, host='localhost', port=6379):
        self.host = host
        self.port = port
        self.sock = None
        
    def connect(self):
        """Connect to Redis server"""
        try:
            self.sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
            self.sock.connect((self.host, self.port))
            print(f"✓ Connected to Redis at {self.host}:{self.port}")
            return True
        except Exception as e:
            print(f"✗ Failed to connect: {e}")
            return False
    
    def disconnect(self):
        """Disconnect from Redis server"""
        if self.sock:
            self.sock.close()
            print("✓ Disconnected from Redis")
    
    def send_command(self, *args) -> Any:
        """Send a RESP command and parse response"""
        # Encode RESP array
        command = f"*{len(args)}\r\n"
        for arg in args:
            arg_str = str(arg)
            command += f"${len(arg_str)}\r\n{arg_str}\r\n"
        
        try:
            self.sock.sendall(command.encode())
            return self._read_response()
        except Exception as e:
            return f"ERROR: {e}"
    
    def _read_response(self) -> Any:
        """Read and parse RESP response"""
        first_byte = self.sock.recv(1).decode()
        
        if first_byte == '+':  # Simple string
            return self._read_line()
        elif first_byte == '-':  # Error
            return f"ERROR: {self._read_line()}"
        elif first_byte == ':':  # Integer
            return int(self._read_line())
        elif first_byte == '$':  # Bulk string
            length = int(self._read_line())
            if length == -1:
                return None
            data = self.sock.recv(length).decode()
            self.sock.recv(2)  # \r\n
            return data
        elif first_byte == '*':  # Array
            count = int(self._read_line())
            if count == -1:
                return None
            return [self._read_response() for _ in range(count)]
        else:
            return f"UNKNOWN RESPONSE: {first_byte}"
    
    def _read_line(self) -> str:
        """Read a line until \r\n"""
        line = b''
        while True:
            char = self.sock.recv(1)
            if char == b'\r':
                self.sock.recv(1)  # \n
                break
            line += char
        return line.decode()


class RedisCommandTester:
    """Test suite for Redis commands"""
    
    def __init__(self, client: RedisClient):
        self.client = client
        self.passed = 0
        self.failed = 0
        self.errors = []
    
    def test(self, name: str, command: List[str], expected: Any = None, check_fn=None):
        """Run a single test"""
        try:
            result = self.client.send_command(*command)
            
            # Check if result is an error
            if isinstance(result, str) and result.startswith("ERROR:"):
                print(f"  ✗ {name}: {result}")
                self.failed += 1
                self.errors.append(f"{name}: {result}")
                return False
            
            # Custom check function
            if check_fn:
                if check_fn(result):
                    print(f"  ✓ {name}: {result}")
                    self.passed += 1
                    return True
                else:
                    print(f"  ✗ {name}: Expected check to pass, got {result}")
                    self.failed += 1
                    self.errors.append(f"{name}: Check failed, got {result}")
                    return False
            
            # Expected value check
            if expected is not None:
                if result == expected:
                    print(f"  ✓ {name}: {result}")
                    self.passed += 1
                    return True
                else:
                    print(f"  ✗ {name}: Expected {expected}, got {result}")
                    self.failed += 1
                    self.errors.append(f"{name}: Expected {expected}, got {result}")
                    return False
            else:
                print(f"  ✓ {name}: {result}")
                self.passed += 1
                return True
                
        except Exception as e:
            print(f"  ✗ {name}: Exception - {e}")
            self.failed += 1
            self.errors.append(f"{name}: {e}")
            return False
    
    def section(self, title: str):
        """Print section header"""
        print(f"\n{'='*60}")
        print(f"{title}")
        print('='*60)
    
    def run_all_tests(self):
        """Run comprehensive test suite"""
        
        # Clear database first
        self.section("CLEANUP")
        self.test("FLUSHALL", ["FLUSHALL"], "OK")
        
        # STRING COMMANDS
        self.section("STRING COMMANDS")
        self.test("SET key", ["SET", "mykey", "Hello"], "OK")
        self.test("GET key", ["GET", "mykey"], "Hello")
        self.test("SET overwrite", ["SET", "mykey", "World"], "OK")
        self.test("GET updated", ["GET", "mykey"], "World")
        self.test("GET non-existent", ["GET", "nonexistent"], None)
        self.test("DEL key", ["DEL", "mykey"], 1)
        self.test("DEL non-existent", ["DEL", "nonexistent"], 0)
        self.test("EXISTS true", ["SET", "key1", "val1"], "OK")
        self.client.send_command("SET", "key1", "val1")
        self.test("EXISTS check", ["EXISTS", "key1"], 1)
        self.test("EXISTS false", ["EXISTS", "key999"], 0)
        
        # INCR/DECR
        self.test("SET counter", ["SET", "counter", "10"], "OK")
        self.test("INCR", ["INCR", "counter"], 11)
        self.test("INCRBY 5", ["INCRBY", "counter", "5"], 16)
        self.test("DECR", ["DECR", "counter"], 15)
        self.test("DECRBY 3", ["DECRBY", "counter", "3"], 12)
        
        # EXPIRY
        self.test("SET with expiry", ["SET", "tempkey", "temp"], "OK")
        self.test("EXPIRE 2s", ["EXPIRE", "tempkey", "2"], 1)
        self.test("TTL check", ["TTL", "tempkey"], check_fn=lambda x: x > 0 and x <= 2)
        
        # LIST COMMANDS
        self.section("LIST COMMANDS")
        self.test("LPUSH", ["LPUSH", "mylist", "world"], 1)
        self.test("LPUSH 2", ["LPUSH", "mylist", "hello"], 2)
        self.test("LRANGE all", ["LRANGE", "mylist", "0", "-1"], ["hello", "world"])
        self.test("RPUSH", ["RPUSH", "mylist", "!"], 3)
        self.test("LLEN", ["LLEN", "mylist"], 3)
        self.test("LINDEX 0", ["LINDEX", "mylist", "0"], "hello")
        self.test("LINDEX 1", ["LINDEX", "mylist", "1"], "world")
        self.test("LPOP", ["LPOP", "mylist"], "hello")
        self.test("RPOP", ["RPOP", "mylist"], "!")
        self.test("LLEN after pop", ["LLEN", "mylist"], 1)
        
        # LIST ADVANCED
        self.test("RPUSH multi", ["RPUSH", "list2", "a", "b", "c", "d", "e"], 5)
        self.test("LSET", ["LSET", "list2", "2", "C"], "OK")
        self.test("LRANGE check", ["LRANGE", "list2", "0", "-1"], ["a", "b", "C", "d", "e"])
        self.test("LTRIM", ["LTRIM", "list2", "1", "3"], "OK")
        self.test("LRANGE trimmed", ["LRANGE", "list2", "0", "-1"], ["b", "C", "d"])
        
        # HASH COMMANDS
        self.section("HASH COMMANDS")
        self.test("HSET", ["HSET", "user:1", "name", "Alice"], 1)
        self.test("HSET age", ["HSET", "user:1", "age", "30"], 1)
        self.test("HGET name", ["HGET", "user:1", "name"], "Alice")
        self.test("HGET age", ["HGET", "user:1", "age"], "30")
        self.test("HEXISTS true", ["HEXISTS", "user:1", "name"], 1)
        self.test("HEXISTS false", ["HEXISTS", "user:1", "email"], 0)
        self.test("HLEN", ["HLEN", "user:1"], 2)
        self.test("HKEYS", ["HKEYS", "user:1"], check_fn=lambda x: set(x) == {"name", "age"})
        self.test("HVALS", ["HVALS", "user:1"], check_fn=lambda x: set(x) == {"Alice", "30"})
        self.test("HDEL", ["HDEL", "user:1", "age"], 1)
        self.test("HLEN after del", ["HLEN", "user:1"], 1)
        
        # HASH ADVANCED
        self.test("HSET user2", ["HSET", "user:2", "name", "Bob"], 1)
        self.test("HSET age", ["HSET", "user:2", "age", "25"], 1)
        self.test("HSET city", ["HSET", "user:2", "city", "NYC"], 1)
        self.test("HMGET", ["HMGET", "user:2", "name", "age"], ["Bob", "25"])
        self.test("HINCRBY", ["HINCRBY", "user:2", "age", "1"], 26)
        
        # SET COMMANDS
        self.section("SET COMMANDS")
        self.test("SADD", ["SADD", "myset", "apple"], 1)
        self.test("SADD multi", ["SADD", "myset", "banana", "cherry"], 2)
        self.test("SISMEMBER true", ["SISMEMBER", "myset", "apple"], 1)
        self.test("SISMEMBER false", ["SISMEMBER", "myset", "grape"], 0)
        self.test("SCARD", ["SCARD", "myset"], 3)
        self.test("SMEMBERS", ["SMEMBERS", "myset"], check_fn=lambda x: set(x) == {"apple", "banana", "cherry"})
        self.test("SREM", ["SREM", "myset", "banana"], 1)
        self.test("SCARD after rem", ["SCARD", "myset"], 2)
        
        # SET OPERATIONS
        self.test("SADD set1", ["SADD", "set1", "a", "b", "c"], 3)
        self.test("SADD set2", ["SADD", "set2", "b", "c", "d"], 3)
        self.test("SUNION", ["SUNION", "set1", "set2"], check_fn=lambda x: set(x) == {"a", "b", "c", "d"})
        self.test("SINTER", ["SINTER", "set1", "set2"], check_fn=lambda x: set(x) == {"b", "c"})
        self.test("SDIFF", ["SDIFF", "set1", "set2"], ["a"])
        
        # SORTED SET COMMANDS
        self.section("SORTED SET COMMANDS")
        self.test("ZADD", ["ZADD", "scores", "100", "Alice"], 1)
        self.test("ZADD multi", ["ZADD", "scores", "85", "Bob", "92", "Charlie"], 2)
        self.test("ZCARD", ["ZCARD", "scores"], 3)
        self.test("ZSCORE", ["ZSCORE", "scores", "Alice"], 100.0)
        self.test("ZRANK", ["ZRANK", "scores", "Alice"], 2)  # Highest score = rank 2 (0-indexed)
        self.test("ZREVRANK", ["ZREVRANK", "scores", "Alice"], 0)  # Highest in reverse
        
        # ZSET RANGE
        self.test("ZRANGE", ["ZRANGE", "scores", "0", "-1"], 
                 check_fn=lambda x: x == ["Bob", "Charlie", "Alice"])
        self.test("ZREVRANGE", ["ZREVRANGE", "scores", "0", "-1"], 
                 check_fn=lambda x: x == ["Alice", "Charlie", "Bob"])
        self.test("ZINCRBY", ["ZINCRBY", "scores", "5", "Bob"], 90.0)
        self.test("ZREM", ["ZREM", "scores", "Charlie"], 1)
        self.test("ZCARD after rem", ["ZCARD", "scores"], 2)
        
        # ZSET ADVANCED
        self.test("ZADD scores2", ["ZADD", "scores2", "10", "a", "20", "b", "30", "c", "40", "d"], 4)
        self.test("ZCOUNT", ["ZCOUNT", "scores2", "15", "35"], 2)  # b and c
        self.test("ZREMRANGEBYRANK", ["ZREMRANGEBYRANK", "scores2", "0", "1"], 2)  # Remove a and b
        self.test("ZCARD after rankrem", ["ZCARD", "scores2"], 2)
        
        # BITMAP COMMANDS
        self.section("BITMAP COMMANDS")
        self.test("SETBIT", ["SETBIT", "mybitmap", "7", "1"], 0)
        self.test("GETBIT set", ["GETBIT", "mybitmap", "7"], 1)
        self.test("GETBIT unset", ["GETBIT", "mybitmap", "100"], 0)
        self.test("SETBIT multi", ["SETBIT", "mybitmap", "10", "1"], 0)
        self.test("BITCOUNT", ["BITCOUNT", "mybitmap"], 2)
        self.test("BITPOS 1", ["BITPOS", "mybitmap", "1"], 7)
        
        # HYPERLOGLOG COMMANDS
        self.section("HYPERLOGLOG COMMANDS")
        self.test("PFADD", ["PFADD", "hll", "a", "b", "c"], 1)
        self.test("PFCOUNT", ["PFCOUNT", "hll"], check_fn=lambda x: x >= 3)  # Approximate
        self.test("PFADD dupe", ["PFADD", "hll", "a", "b", "c"], 0)  # No new elements
        self.test("PFADD new", ["PFADD", "hll2", "d", "e", "f"], 1)
        self.test("PFMERGE", ["PFMERGE", "hll3", "hll", "hll2"], "OK")
        self.test("PFCOUNT merged", ["PFCOUNT", "hll3"], check_fn=lambda x: x >= 6)
        
        # BLOOM FILTER COMMANDS
        self.section("BLOOM FILTER COMMANDS")
        self.test("BF.RESERVE", ["BF.RESERVE", "mybloom", "0.01", "1000"], "OK")
        self.test("BF.ADD", ["BF.ADD", "mybloom", "apple"], 1)
        self.test("BF.EXISTS true", ["BF.EXISTS", "mybloom", "apple"], 1)
        self.test("BF.EXISTS false", ["BF.EXISTS", "mybloom", "grape"], 0)
        self.test("BF.MADD", ["BF.MADD", "mybloom", "banana", "cherry"], [1, 1])
        self.test("BF.MEXISTS", ["BF.MEXISTS", "mybloom", "banana", "grape"], [1, 0])
        
        # GEO COMMANDS
        self.section("GEO COMMANDS")
        self.test("GEOADD", ["GEOADD", "cities", "13.361389", "38.115556", "Palermo"], 1)
        self.test("GEOADD multi", ["GEOADD", "cities", "15.087269", "37.502669", "Catania"], 1)
        self.test("GEODIST", ["GEODIST", "cities", "Palermo", "Catania", "km"], 
                 check_fn=lambda x: x is not None and 150 < x < 170)
        
        # LUA SCRIPTING
        self.section("LUA SCRIPTING")
        script1 = "return KEYS[1] .. ' ' .. ARGV[1]"
        self.test("EVAL concat", ["EVAL", script1, "1", "Hello", "World"], "Hello World")
        
        script2 = "return redis.call('GET', KEYS[1])"
        self.client.send_command("SET", "luakey", "luavalue")
        self.test("EVAL redis.call", ["EVAL", script2, "1", "luakey"], "luavalue")
        
        script3 = "return {1, 2, 3, 4, 5}"
        self.test("EVAL array", ["EVAL", script3, "0"], [1, 2, 3, 4, 5])
        
        # SCRIPT LOAD and EVALSHA
        self.test("SCRIPT LOAD", ["SCRIPT", "LOAD", script1], 
                 check_fn=lambda x: isinstance(x, str) and len(x) == 40)
        sha = self.client.send_command("SCRIPT", "LOAD", script1)
        if isinstance(sha, str) and len(sha) == 40:
            self.test("EVALSHA", ["EVALSHA", sha, "1", "Foo", "Bar"], "Foo Bar")
            self.test("SCRIPT EXISTS", ["SCRIPT", "EXISTS", sha], [1])
        
        # CLEANUP TESTS
        self.section("CLEANUP & UTILITY")
        self.test("KEYS pattern", ["KEYS"], check_fn=lambda x: isinstance(x, list) and len(x) > 0)
        self.test("FLUSHALL final", ["FLUSHALL"], "OK")
        self.test("KEYS empty", ["KEYS"], [])
        
        # PING TEST
        self.test("PING", ["PING"], "PONG")
        self.test("PING message", ["PING", "Hello"], "Hello")
        
        # SUMMARY
        self.print_summary()
    
    def print_summary(self):
        """Print test summary"""
        total = self.passed + self.failed
        print(f"\n{'='*60}")
        print(f"TEST SUMMARY")
        print('='*60)
        print(f"Total Tests: {total}")
        print(f"✓ Passed: {self.passed} ({self.passed*100//total if total > 0 else 0}%)")
        print(f"✗ Failed: {self.failed} ({self.failed*100//total if total > 0 else 0}%)")
        
        if self.errors:
            print(f"\n{'='*60}")
            print(f"FAILED TESTS:")
            print('='*60)
            for error in self.errors:
                print(f"  • {error}")
        
        print(f"\n{'='*60}")
        if self.failed == 0:
            print("🎉 ALL TESTS PASSED!")
        else:
            print(f"⚠️  {self.failed} TEST(S) FAILED")
        print('='*60)


def main():
    """Main test runner"""
    print("="*60)
    print("REDIS SERVER COMPREHENSIVE TEST SUITE")
    print("="*60)
    print()
    
    # Parse arguments
    host = 'localhost'
    port = 6379
    
    if len(sys.argv) > 1:
        host = sys.argv[1]
    if len(sys.argv) > 2:
        port = int(sys.argv[2])
    
    # Connect to Redis
    client = RedisClient(host, port)
    if not client.connect():
        print("\n❌ Cannot connect to Redis server")
        print(f"   Make sure Redis is running on {host}:{port}")
        print("\nUsage: python test_redis_commands.py [host] [port]")
        sys.exit(1)
    
    # Run tests
    tester = RedisCommandTester(client)
    try:
        tester.run_all_tests()
    except KeyboardInterrupt:
        print("\n\n⚠️  Tests interrupted by user")
    finally:
        client.disconnect()
    
    # Exit with appropriate code
    sys.exit(0 if tester.failed == 0 else 1)


if __name__ == "__main__":
    main()
