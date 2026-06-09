// Copyright 2024-2026 mountainKing Contributors
// Licensed under the Apache License, Version 2.0
// See LICENSE file for details.

package mock

// defaultTables returns pre-defined sample data for the mock adapter.
// These tables provide realistic-looking data so developers can immediately
// query the GraphQL API without configuring an external database.
func defaultTables() map[string][]map[string]any {
	return map[string][]map[string]any{
		"demo_users": {
			{"id": 1, "name": "Alice Chen", "email": "alice@example.com", "role": "admin", "created_at": "2024-01-10 09:00:00"},
			{"id": 2, "name": "Bob Zhang", "email": "bob@example.com", "role": "developer", "created_at": "2024-01-12 14:30:00"},
			{"id": 3, "name": "Carol Wang", "email": "carol@example.com", "role": "analyst", "created_at": "2024-02-01 11:15:00"},
			{"id": 4, "name": "David Li", "email": "david@example.com", "role": "developer", "created_at": "2024-02-15 08:45:00"},
			{"id": 5, "name": "Eva Liu", "email": "eva@example.com", "role": "viewer", "created_at": "2024-03-01 16:20:00"},
		},
		"demo_orders": {
			{"id": 1001, "user_id": 1, "amount": 299.99, "status": "completed", "created_at": "2024-03-01 10:00:00"},
			{"id": 1002, "user_id": 2, "amount": 149.50, "status": "completed", "created_at": "2024-03-02 11:30:00"},
			{"id": 1003, "user_id": 1, "amount": 89.00, "status": "shipped", "created_at": "2024-03-03 09:15:00"},
			{"id": 1004, "user_id": 3, "amount": 450.00, "status": "pending", "created_at": "2024-03-04 14:00:00"},
			{"id": 1005, "user_id": 4, "amount": 32.99, "status": "completed", "created_at": "2024-03-05 16:45:00"},
			{"id": 1006, "user_id": 2, "amount": 199.00, "status": "cancelled", "created_at": "2024-03-06 08:30:00"},
			{"id": 1007, "user_id": 5, "amount": 75.50, "status": "shipped", "created_at": "2024-03-07 12:00:00"},
			{"id": 1008, "user_id": 3, "amount": 520.00, "status": "completed", "created_at": "2024-03-08 10:20:00"},
			{"id": 1009, "user_id": 1, "amount": 15.99, "status": "pending", "created_at": "2024-03-09 17:10:00"},
			{"id": 1010, "user_id": 4, "amount": 680.00, "status": "completed", "created_at": "2024-03-10 09:00:00"},
		},
		"demo_metrics": {
			{"timestamp": "2024-03-10 00:00:00", "cpu_usage": 23.5, "memory_usage": 45.2, "request_count": 120},
			{"timestamp": "2024-03-10 01:00:00", "cpu_usage": 18.2, "memory_usage": 44.8, "request_count": 85},
			{"timestamp": "2024-03-10 02:00:00", "cpu_usage": 12.1, "memory_usage": 43.5, "request_count": 42},
			{"timestamp": "2024-03-10 03:00:00", "cpu_usage": 10.8, "memory_usage": 43.1, "request_count": 28},
			{"timestamp": "2024-03-10 04:00:00", "cpu_usage": 11.5, "memory_usage": 43.3, "request_count": 35},
			{"timestamp": "2024-03-10 05:00:00", "cpu_usage": 15.3, "memory_usage": 44.0, "request_count": 67},
			{"timestamp": "2024-03-10 06:00:00", "cpu_usage": 28.7, "memory_usage": 46.5, "request_count": 198},
			{"timestamp": "2024-03-10 07:00:00", "cpu_usage": 45.2, "memory_usage": 52.3, "request_count": 456},
			{"timestamp": "2024-03-10 08:00:00", "cpu_usage": 62.8, "memory_usage": 58.1, "request_count": 723},
			{"timestamp": "2024-03-10 09:00:00", "cpu_usage": 78.4, "memory_usage": 64.7, "request_count": 891},
			{"timestamp": "2024-03-10 10:00:00", "cpu_usage": 82.1, "memory_usage": 67.3, "request_count": 1024},
			{"timestamp": "2024-03-10 11:00:00", "cpu_usage": 79.6, "memory_usage": 66.8, "request_count": 978},
			{"timestamp": "2024-03-10 12:00:00", "cpu_usage": 71.3, "memory_usage": 62.4, "request_count": 856},
			{"timestamp": "2024-03-10 13:00:00", "cpu_usage": 68.9, "memory_usage": 61.0, "request_count": 812},
			{"timestamp": "2024-03-10 14:00:00", "cpu_usage": 75.2, "memory_usage": 63.5, "request_count": 934},
			{"timestamp": "2024-03-10 15:00:00", "cpu_usage": 73.8, "memory_usage": 62.9, "request_count": 901},
			{"timestamp": "2024-03-10 16:00:00", "cpu_usage": 65.4, "memory_usage": 59.2, "request_count": 756},
			{"timestamp": "2024-03-10 17:00:00", "cpu_usage": 52.1, "memory_usage": 54.6, "request_count": 589},
			{"timestamp": "2024-03-10 18:00:00", "cpu_usage": 38.7, "memory_usage": 49.8, "request_count": 342},
			{"timestamp": "2024-03-10 19:00:00", "cpu_usage": 29.3, "memory_usage": 47.1, "request_count": 215},
		},
	}
}
