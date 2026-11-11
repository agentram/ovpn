package local

import "context"

// UserAssignmentIDs gives the desktop stable node references without client identities.
func (s *Store) UserAssignmentIDs(ctx context.Context) (map[string][]int64, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT username,server_id FROM users ORDER BY username,server_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := map[string][]int64{}
	for rows.Next() {
		var username string
		var id int64
		if err := rows.Scan(&username, &id); err != nil {
			return nil, err
		}
		result[username] = append(result[username], id)
	}
	return result, rows.Err()
}
