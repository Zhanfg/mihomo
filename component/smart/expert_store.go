package smart

import "errors"

func expertBankKey(config, group string) string {
	return FormatDBKey(KeyTypeExperts, config, group)
}

func (s *Store) LoadExpertBank(group, config string) *ExpertBank {
	if s == nil {
		return NewExpertBank()
	}
	data, err := s.DBViewGetItem(expertBankKey(config, group))
	if err != nil {
		return NewExpertBank()
	}
	return LoadExpertBank(data)
}

func (s *Store) SaveExpertBank(group, config string, bank *ExpertBank) error {
	if s == nil || bank == nil {
		return nil
	}
	data, err := bank.MarshalBounded()
	if err != nil {
		return err
	}
	if len(data) == 0 {
		return nil
	}
	if len(data) > ExpertMaxPersistBytes {
		return errors.New("Smart expert bank persistence budget exceeded")
	}
	if err := s.DBBatchPutItem(expertBankKey(config, group), data); err != nil {
		return err
	}
	bank.MarkClean()
	return nil
}

func (s *Store) DeleteExpertBank(group, config string) error {
	if s == nil {
		return nil
	}
	return s.DBBatchDeletePrefix([]string{expertBankKey(config, group)}, true)
}
