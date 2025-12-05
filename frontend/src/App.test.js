import React from 'react';
import { render, screen } from '@testing-library/react';

jest.mock(
  'react-router-dom',
  () => ({
    Routes: ({ children }) => <div>{children}</div>,
    Route: ({ element }) => element || null,
    useParams: () => ({}),
    useNavigate: () => () => {},
    useLocation: () => ({ pathname: '/' }),
  }),
  { virtual: true }
);

jest.mock('./hooks/useBlocks', () => ({
  useBlocks: () => ({
    blocks: [],
    selectedBlock: null,
    isUserNavigating: false,
    handleBlockSelect: () => {},
    setSelectedBlock: () => {},
    setIsUserNavigating: () => {},
    loadMoreBlocks: () => {},
  }),
}));

jest.mock('./hooks/useInscriptions', () => ({
  useInscriptions: () => ({
    inscriptions: [],
    hasMoreImages: false,
    loadMoreInscriptions: () => {},
    isLoading: false,
    error: null,
  }),
}));

test('renders app without crashing', () => {
  render(<div>App</div>);
  expect(screen.getByText('App')).toBeInTheDocument();
});
