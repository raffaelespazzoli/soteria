/* eslint-env node */

import * as path from 'path';
import { Configuration as WebpackConfiguration } from 'webpack';
import * as webpack from 'webpack';

const HtmlWebpackPlugin = require('html-webpack-plugin');

const isProd = process.env.NODE_ENV === 'production';

const config: WebpackConfiguration = {
  mode: isProd ? 'production' : 'development',
  entry: path.resolve(__dirname, 'standalone/main.tsx'),
  context: path.resolve(__dirname, 'src'),
  output: {
    path: path.resolve(__dirname, 'dist-standalone'),
    filename: isProd ? '[name]-bundle-[contenthash].min.js' : '[name]-bundle.js',
    publicPath: '/',
    clean: true,
  },
  resolve: {
    extensions: ['.ts', '.tsx', '.js', '.jsx'],
  },
  module: {
    rules: [
      {
        test: /\.(jsx?|tsx?)$/,
        exclude: /\/node_modules\//,
        use: [
          {
            loader: 'ts-loader',
            options: {
              configFile: path.resolve(__dirname, 'tsconfig.json'),
              transpileOnly: true,
            },
          },
        ],
      },
      {
        test: /\.(css)$/,
        use: ['style-loader', 'css-loader'],
      },
      {
        test: /\.(png|jpg|jpeg|gif|svg|woff2?|ttf|eot|otf)(\?.*$|$)/,
        type: 'asset/resource',
        generator: {
          filename: isProd ? 'assets/[contenthash][ext]' : 'assets/[name][ext]',
        },
      },
      {
        test: /\.(m?js)$/,
        resolve: {
          fullySpecified: false,
        },
      },
    ],
  },
  plugins: [
    new webpack.DefinePlugin({
      __STANDALONE__: JSON.stringify(true),
    }),
    new HtmlWebpackPlugin({
      template: path.resolve(__dirname, 'standalone/index.html'),
      filename: 'index.html',
    }),
  ],
  externals: {
    '@openshift-console/dynamic-plugin-sdk': 'var {}',
    '@openshift-console/dynamic-plugin-sdk-webpack': 'var {}',
  },
  devtool: isProd ? false : 'source-map',
  optimization: {
    chunkIds: isProd ? 'deterministic' : 'named',
    minimize: isProd,
  },
};

export default config;
