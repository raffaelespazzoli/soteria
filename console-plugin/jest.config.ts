import type { Config } from 'jest';

const config: Config = {
  testEnvironment: 'jsdom',
  transform: {
    '^.+\\.tsx?$': [
      'ts-jest',
      {
        tsconfig: {
          esModuleInterop: true,
          jsx: 'react-jsx',
          types: ['jest', '@testing-library/jest-dom', 'jest-axe'],
        },
      },
    ],
  },
  moduleFileExtensions: ['ts', 'tsx', 'js', 'jsx', 'json'],
  moduleNameMapper: {
    '\\.(css|less|scss)$': 'identity-obj-proxy',
    '\\.(png|jpg|jpeg|gif|svg)$': '<rootDir>/tests/__mocks__/fileMock.ts',
  },
  testMatch: ['<rootDir>/tests/**/*.test.{ts,tsx}'],
  setupFilesAfterEnv: ['<rootDir>/tests/setup.ts'],
};

export default config;
