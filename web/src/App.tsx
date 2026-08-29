import { BrowserRouter, Navigate, Route, Routes } from 'react-router-dom'
import Login from './pages/Login'
import Register from './pages/Register'
import TestSetList from './pages/TestSetList'
import Dashboard from './pages/Dashboard'
import TestSetDetail from './pages/TestSetDetail'
import FlowEditor from './pages/FlowEditor'
import Providers from './pages/Providers'
import { isAuthed } from './store/session'

function RequireAuth({ children }: { children: React.ReactNode }) {
  if (!isAuthed()) return <Navigate to="/login" replace />
  return <>{children}</>
}

function App() {
  return (
    <BrowserRouter>
      <Routes>
        <Route path="/login" element={<Login />} />
        <Route path="/register" element={<Register />} />
        <Route
          path="/dashboard"
          element={
            <RequireAuth>
              <Dashboard />
            </RequireAuth>
          }
        />
        <Route
          path="/test-sets"
          element={
            <RequireAuth>
              <TestSetList />
            </RequireAuth>
          }
        />
        <Route
          path="/test-sets/:id"
          element={
            <RequireAuth>
              <TestSetDetail />
            </RequireAuth>
          }
        />
        <Route
          path="/flows/:flowID"
          element={
            <RequireAuth>
              <FlowEditor />
            </RequireAuth>
          }
        />
        <Route
          path="/providers"
          element={
            <RequireAuth>
              <Providers />
            </RequireAuth>
          }
        />
        <Route path="*" element={<Navigate to="/dashboard" replace />} />
      </Routes>
    </BrowserRouter>
  )
}

export default App
